package cloudsql

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	cloudcost_exporter "github.com/grafana/cloudcost-exporter"
	"github.com/grafana/cloudcost-exporter/pkg/google/client"
	"github.com/grafana/cloudcost-exporter/pkg/provider"
	"github.com/grafana/cloudcost-exporter/pkg/utils"
	"github.com/prometheus/client_golang/prometheus"
	sqladmin "google.golang.org/api/sqladmin/v1beta4"
)

type Config struct {
	Projects       string
	ScrapeInterval time.Duration
}

type Collector struct {
	projects   []string
	regions    []string
	gcpClient  client.Client
	config     *Config
	pricingMap *pricingMap
	logger     *slog.Logger
}

const (
	subsystem           = "gcp_cloudsql"
	CostRefreshInterval = 24 * time.Hour
)

var (
	initRetryAttempts     = 3
	initRetryInitialDelay = 1 * time.Second
	initRetryMaxDelay     = 30 * time.Second
)

var (
	HourlyGaugeDesc = utils.GenerateDesc(
		cloudcost_exporter.MetricPrefix,
		subsystem,
		"cost_usd_per_hour",
		"Hourly cost of GCP cloudsql instances by instance name and region. Cost represented in USD/hour",
		[]string{"instance", "region"},
	)
)

func New(ctx context.Context, config *Config, logger *slog.Logger, gcpClient client.Client) (*Collector, error) {
	logger = logger.With("collector", "cloudsql")
	pm := newPricingMap(logger, gcpClient)
	projects := strings.Split(config.Projects, ",")
	regions := client.RegionsForProjects(gcpClient, projects, logger)

	if err := utils.Retry(initRetryAttempts, initRetryInitialDelay, initRetryMaxDelay, client.IsRetryableError, func() error {
		return pm.getSKus(ctx)
	}); err != nil {
		return nil, fmt.Errorf("failed to initialise Cloud SQL pricing: %w", err)
	}

	go func() {
		ticker := time.NewTicker(CostRefreshInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := pm.getSKus(ctx); err != nil {
					logger.Error("failed to refresh Cloud SQL pricing SKUs", "error", err)
				}
			}
		}
	}()

	return &Collector{
		gcpClient:  gcpClient,
		config:     config,
		pricingMap: pm,
		projects:   projects,
		regions:    regions,
		logger:     logger,
	}, nil
}

func (c *Collector) Regions() []string {
	return c.regions
}

func (c *Collector) Describe(ch chan<- *prometheus.Desc) error {
	return nil
}

func (c *Collector) Collect(ctx context.Context, ch chan<- prometheus.Metric) error {
	instances, err := c.getAllCloudSQL(ctx)
	if err != nil {
		return err
	}

	for _, instance := range instances {
		price, err := c.pricingMap.matchInstancePrice(instance)
		if err != nil {
			c.logger.Warn("failed to match price for instance",
				"name", instance.Name,
				"region", instance.Region,
				"tier", instance.Settings.Tier,
				"error", err)
			continue
		}

		metric := prometheus.MustNewConstMetric(
			HourlyGaugeDesc,
			prometheus.GaugeValue,
			price.pricePerHour,
			instance.ConnectionName,
			instance.Region,
		)
		ch <- metric
	}

	return nil
}

func (c *Collector) Name() string {
	return "cloudsql"
}

func (c *Collector) Register(registry provider.Registry) error {
	c.logger.Info("Registering CloudSQL metrics")
	return nil
}

func (c *Collector) getAllCloudSQL(ctx context.Context) ([]*sqladmin.DatabaseInstance, error) {
	var allCloudSQLInfo = []*sqladmin.DatabaseInstance{}
	seenInstances := make(map[string]bool)
	for _, project := range c.projects {
		cloudSQLInstances, err := c.gcpClient.ListSQLInstances(ctx, project)
		if err != nil {
			c.logger.Error("error listing sql instances for project", "project", project, "error", err)
			return nil, err
		}
		for _, instance := range cloudSQLInstances {
			if instance.ConnectionName != "" {
				if seenInstances[instance.ConnectionName] {
					continue
				}
				seenInstances[instance.ConnectionName] = true
			}
			allCloudSQLInfo = append(allCloudSQLInfo, instance)
		}
	}
	return allCloudSQLInfo, nil
}
