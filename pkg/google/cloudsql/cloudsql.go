package cloudsql

import (
	"context"
	"log"
	"log/slog"
	"time"

	cloudcost_exporter "github.com/grafana/cloudcost-exporter"
	"github.com/grafana/cloudcost-exporter/pkg/google/client"
	"github.com/grafana/cloudcost-exporter/pkg/provider"
	"github.com/grafana/cloudcost-exporter/pkg/utils"
	"github.com/prometheus/client_golang/prometheus"
)

type Config struct {
	ProjectId      string
	ScrapeInterval time.Duration
	Logger         *slog.Logger
}

type Collector struct {
	gcpClient     client.Client
	config        *Config
	PricingClient *pricingMap
	usageTicker   *time.Ticker
	costTicker    *time.Ticker
}

const (
	subsystem            = "gcp_cloudsql"
	UsageRefreshInterval = 24 * time.Hour
	CostRefreshInterval  = 24 * 30 * time.Hour
)

var (
	HourlyGaugeDesc = utils.GenerateDesc(
		cloudcost_exporter.MetricPrefix,
		subsystem,
		"cost_usd_per_hour",
		"Hourly cost of GCP cloudsql instances by id, region and sku. Cost represented in USD/hour",
		[]string{"id", "region", "sku"},
	)
)

var (
	UsageGaugeDesc = utils.GenerateDesc(
		cloudcost_exporter.MetricPrefix,
		subsystem,
		"usage_usd_per_hour",
		"Hourly usage of GCP cloudsql networking by id, region and sku. Usage represented in GB/hour",
		[]string{"id", "region", "sku"},
	)
)

func New(config *Config, gcpClient client.Client) (*Collector, error) {
	logger := config.Logger.With("logger", "cloudsql")
	pm := newPricingMap(logger, gcpClient)
	// get prices
	// if err := pm.populate(ctx); err != nil {
	// 	return nil, err
	// }
	usageTicker := time.NewTicker(UsageRefreshInterval)
	costTicker := time.NewTicker(CostRefreshInterval)
	// go func(ctx context.Context) {
	// 	for {
	// 		select {
	// 		case <-ctx.Done():
	// 			return
	// 		case <-usageTicker.C:
	// 			if err := pm.populate(ctx); err != nil {
	// 				logger.Error("failed to refresh pricing map", "error", err)
	// 			}
	// 		case <-costTicker.C:
	// 			if err := pm.populate(ctx); err != nil {
	// 				logger.Error("failed to refresh pricing map", "error", err)
	// 			}
	// 		}
	// 	}
	// }(ctx)
	return &Collector{
		gcpClient:     gcpClient,
		config:        config,
		PricingClient: pm,
		usageTicker:   usageTicker,
		costTicker:    costTicker,
	}, nil
}

func (c *Collector) Describe(ch chan<- *prometheus.Desc) error {
	return nil
}

func (c *Collector) Collect(ch chan<- prometheus.Metric) error {
	ctx := context.Background()
	logger := c.config.Logger.With("logger", "cloudsql")

	// Get all SQL instances
	instances, err := c.gcpClient.ListSQLInstances(ctx, c.config.ProjectId)
	if err != nil {
		return err
	}

	// Match each instance with its price and emit metrics
	for _, instance := range instances {
		price, err := c.PricingClient.matchInstancePrice(instance)
		if err != nil {
			logger.Warn("failed to match price for instance",
				"instance", instance.Name,
				"region", instance.Region,
				"tier", instance.Settings.Tier,
				"error", err)
			continue
		}

		// Determine SKU label
		skuLabel := price.SKUID
		if price.isCustom {
			skuLabel = "custom"
		}

		// Emit cost metric
		metric := prometheus.MustNewConstMetric(
			HourlyGaugeDesc,
			prometheus.GaugeValue,
			price.PricePerHour,
			instance.Name,
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

// func (c *Collector) getCloudSQLInfo() ([]CloudSQLInfo, error) {
// 	var allCloudSQLInfo = []CloudSQLInfo{}
// 	var mu sync.Mutex

// 	// Process projects sequentially
// 	for _, project := range c.config.ProjectId {
// 		regions, err := c.gcpClient.GetRegions(project)
// 		if err != nil {
// 			c.logger.Error("error getting regions for project", "project", project, "error", err)
// 			continue
// 		}
// 		for _, region := range regions {
// 			cloudSQLInstances, err := c.gcpClient.ListSQLInstances(project, region.Name)
// 			if err != nil {
// 				c.logger.Error("error listing sql instances for project", "project", project, "region", region.Name, "error", err)
// 				continue
// 			}
// 		}
// 	}
// 	return allCloudSQLInfo, nil
// }
