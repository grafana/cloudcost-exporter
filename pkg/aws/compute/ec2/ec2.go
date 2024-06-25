package ec2

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/cloudcost-exporter/pkg/aws/compute"
	ec2client "github.com/grafana/cloudcost-exporter/pkg/aws/services/ec2"
	pricingClient "github.com/grafana/cloudcost-exporter/pkg/aws/services/pricing"
	"github.com/grafana/cloudcost-exporter/pkg/provider"
)

const (
	subsystem = "aws_ec2"
)

var (
	ErrClientNotFound = errors.New("no client found")

	ErrGeneratePricingMap = errors.New("error generating pricing map")
)

// Collector is a prometheus collector that collects metrics from AWS EKS clusters.
type Collector struct {
	Region          string
	Regions         []ec2Types.Region
	Profile         string
	Profiles        []string
	ScrapeInterval  time.Duration
	pricingService  pricingClient.Pricing
	ec2Client       ec2client.EC2
	NextScrape      time.Time
	ec2RegionClient map[string]ec2client.EC2
	logger          *slog.Logger
	context         context.Context
	pricingMap      *compute.StructuredPricingMap
}

type Config struct {
	Regions []ec2Types.Region
	Logger  *slog.Logger
}

// CollectMetrics is a no-op function that satisfies the provider.Collector interface.
// Deprecated: CollectMetrics is deprecated and will be removed in a future release.
func (c *Collector) CollectMetrics(_ chan<- prometheus.Metric) float64 {
	return 0
}

// Collect satisfies the provider.Collector interface.
func (c *Collector) Collect(_ chan<- prometheus.Metric) error {
	c.logger.LogAttrs(c.context, slog.LevelInfo, "Collecting Metrics")
	if c.pricingMap == nil || time.Now().After(c.NextScrape) {
		now := time.Now()
		c.logger.LogAttrs(c.context, slog.LevelInfo, "Generating EC2 Pricing Map")
		var prices []string
		var spotPrices []ec2Types.SpotPrice
		eg := new(errgroup.Group)
		eg.SetLimit(5)
		m := sync.Mutex{}
		for _, region := range c.Regions {
			eg.Go(func() error {
				c.logger.LogAttrs(c.context, slog.LevelDebug, "Getting EC2 on demand prices for region", slog.String("region", *region.RegionName))
				priceList, err := compute.ListOnDemandPrices(context.TODO(), *region.RegionName, c.pricingService)
				if err != nil {
					return fmt.Errorf("%w: %w", compute.ErrListOnDemandPrices, err)
				}

				if c.ec2RegionClient[*region.RegionName] == nil {
					return ErrClientNotFound
				}
				client := c.ec2RegionClient[*region.RegionName]
				c.logger.LogAttrs(c.context, slog.LevelDebug, "Getting EC2 spot prices for region", slog.String("region", *region.RegionName))
				spotPriceList, err := compute.ListSpotPrices(context.TODO(), client)
				if err != nil {
					return fmt.Errorf("%w: %w", compute.ErrListSpotPrices, err)
				}
				m.Lock()
				spotPrices = append(spotPrices, spotPriceList...)
				prices = append(prices, priceList...)
				m.Unlock()
				return nil
			})
		}

		err := eg.Wait()
		if err != nil {
			return err
		}
		c.pricingMap = compute.NewStructuredPricingMap()
		if err := c.pricingMap.GeneratePricingMap(prices, spotPrices); err != nil {
			return fmt.Errorf("%w: %w", ErrGeneratePricingMap, err)
		}
		c.NextScrape = time.Now().Add(c.ScrapeInterval)
		c.logger.LogAttrs(c.context, slog.LevelInfo, "Generated EC2 Pricing Map",
			slog.Duration("duration", time.Since(now)),
		)
	}
	return nil
}

func (c *Collector) Describe(_ chan<- *prometheus.Desc) error {
	c.logger.LogAttrs(c.context, slog.LevelInfo, "TODO - Implement ec2.Describe")
	return nil
}

func (c *Collector) Name() string {
	return subsystem
}

// New creates an AWS EC2 collector.
func New(ctx context.Context, config *Config, ps pricingClient.Pricing, ec2s ec2client.EC2, regionClientMap map[string]ec2client.EC2) *Collector {
	logger := config.Logger.With("collector", "ec2")
	return &Collector{
		pricingService:  ps,
		ec2Client:       ec2s,
		Regions:         config.Regions,
		ec2RegionClient: regionClientMap,
		logger:          logger,
		context:         ctx,
	}
}

// Register is called by the prometheus library to register any static metrics that require persistence.
func (c *Collector) Register(_ provider.Registry) error {
	c.logger.LogAttrs(c.context, slog.LevelInfo, "Registering AWS EC2 collector")
	return nil
}
