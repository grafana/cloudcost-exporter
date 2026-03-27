package natgateway

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"
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
	regions        []string
	PricingStore   pricingstore.PricingStoreRefresher

	logger *slog.Logger
}

func New(ctx context.Context, config *Config) *Collector {
	logger := config.Logger.With("logger", serviceName)

	pricingStore := pricingstore.NewPricingStore(ctx, logger, config.Regions, newPriceFetcher(config.RegionMap))

	go func(ctx context.Context) {
		priceTicker := time.NewTicker(pricingstore.PriceRefreshInterval)
		defer priceTicker.Stop()
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
		regions:        slices.Collect(maps.Keys(config.RegionMap)),
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

func (c *Collector) Regions() []string { return c.regions }

func (c *Collector) Describe(ch chan<- *prometheus.Desc) error {
	ch <- HourlyGaugeDesc
	ch <- DataProcessingGaugeDesc
	return nil
}

// Collect satisfies the provider.Collector interface.
func (c *Collector) Collect(ctx context.Context, ch chan<- prometheus.Metric) error {
	c.logger.LogAttrs(ctx, slog.LevelInfo, "calling collect")

	snapshot := c.PricingStore.Snapshot()
	for region, pricePerUnit := range snapshot.Regions() {
		var (
			hourlyPrice         float64
			dataProcessingPrice float64
		)

		for usageType, price := range pricePerUnit.Entries() {
			if strings.Contains(usageType, NATGatewayHours) {
				// Aggregate all hourly NAT Gateway prices for this region into a single value
				// E.g `USE1-NatGateway-Hours` and `USE1-NatGateway-Hours-Additional`
				hourlyPrice += price
			}
			if strings.Contains(usageType, NATGatewayBytes) {
				// Aggregate all data processing NAT Gateway prices for this region into a single value
				// E.g `USE1-NatGateway-Bytes` and `USE1-NatGateway-Bytes-Additional`
				dataProcessingPrice += price
			}
		}

		// Emit at most one sample per metric/region to satisfy Prometheus uniqueness constraints
		if hourlyPrice > 0 {
			ch <- prometheus.MustNewConstMetric(HourlyGaugeDesc, prometheus.GaugeValue, hourlyPrice, region)
		}
		if dataProcessingPrice > 0 {
			ch <- prometheus.MustNewConstMetric(DataProcessingGaugeDesc, prometheus.GaugeValue, dataProcessingPrice, region)
		}
	}

	return nil
}

func (c *Collector) Register(registry provider.Registry) error { return nil }

func newPriceFetcher(regionMap map[string]awsclient.Client) pricingstore.PriceFetchFunc {
	return func(ctx context.Context, region string) ([]string, error) {
		regionClient, ok := regionMap[region]
		if !ok {
			return nil, fmt.Errorf("no client found for region %s", region)
		}

		return regionClient.ListEC2ServicePrices(ctx, region, NATGatewayFilters)
	}
}
