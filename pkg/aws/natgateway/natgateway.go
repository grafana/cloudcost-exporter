package natgateway

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/sync/errgroup"

	cloudcost_exporter "github.com/grafana/cloudcost-exporter"
	awsclient "github.com/grafana/cloudcost-exporter/pkg/aws/client"
	"github.com/grafana/cloudcost-exporter/pkg/provider"
	"github.com/grafana/cloudcost-exporter/pkg/utils"
)

const (
	serviceName   = "natgateway"
	errGroupLimit = 5
)

var (
	subsystem = fmt.Sprintf("aws_%s", serviceName)

	ErrListPrices         = errors.New("error listing prices")
	ErrClientNotFound     = errors.New("no client found")
	ErrGeneratePricingMap = errors.New("error generating pricing map")

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
	nextScrape     time.Time

	// Pricing fields
	// TODO: Split this into separate struct
	regions            []ec2Types.Region
	pricingMap         *PricingStore
	awsRegionClientMap map[string]awsclient.Client

	logger *slog.Logger
}

func New(config *Config, client awsclient.Client) *Collector {
	logger := config.Logger.With("logger", serviceName)

	return &Collector{
		logger:             logger,
		scrapeInterval:     config.ScrapeInterval,
		regions:            config.Regions,
		awsRegionClientMap: config.RegionMap,
		pricingMap:         NewPricingStore(logger),
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

func (c *Collector) Register(registry provider.Registry) error { return nil }

// Collect satisfies the provider.Collector interface.
func (c *Collector) Collect(ch chan<- prometheus.Metric) error {
	ctx := context.Background()
	c.logger.LogAttrs(ctx, slog.LevelInfo, "calling collect")

	start := time.Now()
	if c.pricingMap == nil || start.After(c.nextScrape) {
		err := c.populatePricingMap(ctx, c.logger)
		if err != nil {
			return fmt.Errorf("error populating pricing map: %w", err)
		}
		c.nextScrape = start.Add(c.scrapeInterval)
	}

	for region, pricePerUnit := range c.pricingMap.pricePerUnitPerRegion {
		for usageType, price := range *pricePerUnit {
			switch {
			case strings.Contains(usageType, NATGatewayHours):
				ch <- prometheus.MustNewConstMetric(HourlyGaugeDesc, prometheus.GaugeValue, price, region)
			case strings.Contains(usageType, NATGatewayBytes):
				ch <- prometheus.MustNewConstMetric(DataProcessingGaugeDesc, prometheus.GaugeValue, price, region)
			}
		}
	}

	c.logger.Info("Finished collect", "subsystem", subsystem, "duration", time.Since(start))
	return nil
}

// CollectMetrics is a no-op function that satisfies the provider.Collector interface.
// Deprecated: CollectMetrics is deprecated and will be removed in a future release.
func (c *Collector) CollectMetrics(ch chan<- prometheus.Metric) float64 {
	return 0
}

func (c *Collector) populatePricingMap(errGroupCtx context.Context, logger *slog.Logger) error {
	logger.LogAttrs(errGroupCtx, slog.LevelInfo, "Refreshing pricing map")
	var prices []string
	eg, errGroupCtx := errgroup.WithContext(errGroupCtx)
	eg.SetLimit(errGroupLimit)
	m := sync.Mutex{}
	for _, region := range c.regions {
		eg.Go(func() error {
			logger.LogAttrs(errGroupCtx, slog.LevelDebug, "fetching pricing info", slog.String("region", *region.RegionName))

			regionClient, ok := c.awsRegionClientMap[*region.RegionName]
			if !ok {
				return ErrClientNotFound
			}

			// TODO: Create a generic ListPrices endpoint
			// that takes a awsPricing.GetProductsInput{}
			// with a helper func to build the input
			priceList, err := regionClient.ListNATGatewayPrices(errGroupCtx, *region.RegionName)
			if err != nil {
				return fmt.Errorf("%w: %w", ErrListPrices, err)
			}

			m.Lock()
			prices = append(prices, priceList...)
			m.Unlock()
			return nil
		})
	}
	err := eg.Wait()
	if err != nil {
		return err
	}

	c.pricingMap = NewPricingStore(logger)
	if err := c.pricingMap.PopulatePriceStore(prices); err != nil {
		return fmt.Errorf("%w: %w", ErrGeneratePricingMap, err)
	}

	return nil
}
