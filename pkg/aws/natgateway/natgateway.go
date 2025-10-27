package natgateway

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/prometheus/client_golang/prometheus"

	cloudcost_exporter "github.com/grafana/cloudcost-exporter"
	awsclient "github.com/grafana/cloudcost-exporter/pkg/aws/client"
	"github.com/grafana/cloudcost-exporter/pkg/aws/pricingstore"
	"github.com/grafana/cloudcost-exporter/pkg/provider"
	"github.com/grafana/cloudcost-exporter/pkg/utils"
)

const (
	serviceName = "natgateway"
)

var (
	subsystem = fmt.Sprintf("aws_%s", serviceName)

	HourlyGaugeDesc = utils.GenerateDesc(
		cloudcost_exporter.MetricPrefix,
		subsystem,
		"hourly_rate_usd_per_hour",
		"Hourly cost of NAT Gateway by region. Cost represented in USD/hour",
		[]string{"region"},
	)
	DataProcessingGaugeDesc = utils.GenerateDesc(
		cloudcost_exporter.MetricPrefix,
		subsystem,
		"data_processing_usd_per_gb",
		"Data processing cost of NAT Gateway by region. Cost represented in USD/GB",
		[]string{"region"},
	)
)

// Collector implements provider.Collector
type Collector struct {
	// Collector fields
	scrapeInterval time.Duration
	PricingStore   pricingstore.PricingStoreRefresher

	logger *slog.Logger
}

func New(ctx context.Context, config *Config) *Collector {
	logger := config.Logger.With("logger", serviceName)

	priceTicker := time.NewTicker(pricingstore.PriceRefreshInterval)
	pricingStore := pricingstore.NewPricingStore(ctx, logger, config.Regions, config.RegionMap, NATGatewayFilters)

	go func(ctx context.Context) {
		for {
			select {
			case <-ctx.Done():
				return
			case <-priceTicker.C:
				logger.LogAttrs(ctx, slog.LevelInfo, "refreshing pricing map")
				pricingStore.PopulatePricingMap(ctx)
			}
		}
	}(ctx)

	return &Collector{
		logger:         logger,
		scrapeInterval: config.ScrapeInterval,
		PricingStore:   pricingStore,
	}
}

type Config struct {
	ScrapeInterval time.Duration
	Regions        []ec2Types.Region
	Logger         *slog.Logger
	RegionMap      map[string]awsclient.Client
}

func (c *Collector) Name() string { return strings.ToUpper(serviceName) }

func (c *Collector) Describe(ch chan<- *prometheus.Desc) error {
	ch <- HourlyGaugeDesc
	ch <- DataProcessingGaugeDesc
	return nil
}

// Collect satisfies the provider.Collector interface.
func (c *Collector) Collect(ch chan<- prometheus.Metric) error {
	c.logger.LogAttrs(context.Background(), slog.LevelInfo, "calling collect")

	for region, pricePerUnit := range c.PricingStore.GetPricePerUnitPerRegion() {
		for usageType, price := range *pricePerUnit {
			switch {
			case strings.Contains(usageType, NATGatewayHours):
				ch <- prometheus.MustNewConstMetric(HourlyGaugeDesc, prometheus.GaugeValue, price, region)
			case strings.Contains(usageType, NATGatewayBytes):
				ch <- prometheus.MustNewConstMetric(DataProcessingGaugeDesc, prometheus.GaugeValue, price, region)
			}
		}
	}

	return nil
}

// CollectMetrics is a no-op function that satisfies the provider.Collector interface.
// Deprecated: CollectMetrics is deprecated and will be removed in a future release.
func (c *Collector) CollectMetrics(ch chan<- prometheus.Metric) float64 {
	return 0
}

func (c *Collector) Register(registry provider.Registry) error { return nil }
