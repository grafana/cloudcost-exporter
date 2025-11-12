package cloudsql

import (
	"context"
	"log"
	"log/slog"
	"os"
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
	gcpClient  client.Client
	config     *Config
	pricingMap *pricingMap
	logger     *slog.Logger
}

const (
	subsystem           = "gcp_cloudsql"
	CostRefreshInterval = 24 * 30 * time.Hour
)

var (
	HourlyGaugeDesc = utils.GenerateDesc(
		cloudcost_exporter.MetricPrefix,
		subsystem,
		"cost_usd_per_hour",
		"Hourly cost of GCP cloudsql instances by instance name, region and sku. Cost represented in USD/hour",
		[]string{"instance", "region", "sku"},
	)
)

func New(config *Config, gcpClient client.Client) (*Collector, error) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	pm := newPricingMap(logger, gcpClient)
	return &Collector{
		gcpClient:  gcpClient,
		config:     config,
		pricingMap: pm,
		projects:   strings.Split(config.Projects, ","),
		logger:     logger,
	}, nil
}

func (c *Collector) Describe(ch chan<- *prometheus.Desc) error {
	return nil
}

func (c *Collector) Collect(ch chan<- prometheus.Metric) error {
	ctx := context.Background()
	logger := c.logger.With("logger", "cloudsql")

	instances, err := c.getAllCloudSQL(ctx)
	if err != nil {
		return err
	}

	for _, instance := range instances {
		price, err := c.pricingMap.matchInstancePrice(instance)
		if err != nil {
			logger.Warn("failed to match price for instance",
				"name", instance.Name,
				"region", instance.Region,
				"tier", instance.Settings.Tier,
				"error", err)
			continue
		}

		skuLabel := price.skuID
		if price.isCustom {
			skuLabel = "custom"
		}

		metric := prometheus.MustNewConstMetric(
			HourlyGaugeDesc,
			prometheus.GaugeValue,
			price.pricePerHour,
			instance.ConnectionName,
			instance.Region,
			skuLabel,
		)
		ch <- metric
	}

	return nil
}

func (c *Collector) Name() string {
	return "cloudsql"
}

func (c *Collector) Register(registry provider.Registry) error {
	log.Printf("Registering CloudSQL metrics")
	return nil
}

func (c *Collector) CollectMetrics(ch chan<- prometheus.Metric) float64 {
	return 0
}

func (c *Collector) getAllCloudSQL(ctx context.Context) ([]*sqladmin.DatabaseInstance, error) {
	var allCloudSQLInfo = []*sqladmin.DatabaseInstance{}

	for _, project := range c.projects {
		regions, err := c.gcpClient.GetRegions(project)
		if err != nil {
			c.logger.Error("error getting regions for project", "project", project, "error", err)
			continue
		}
		for _, region := range regions {
			cloudSQLInstances, err := c.gcpClient.ListSQLInstances(ctx, project)
			if err != nil {
				c.logger.Error("error listing sql instances for project", "project", project, "region", region.Name, "error", err)
				continue
			}
			allCloudSQLInfo = append(allCloudSQLInfo, cloudSQLInstances...)
		}
	}
	return allCloudSQLInfo, nil
}
