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

	cloudcostexporter "github.com/grafana/cloudcost-exporter"
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

var (
	totalHourlyCostDesc = prometheus.NewDesc(
		prometheus.BuildFQName(cloudcostexporter.MetricPrefix, subsystem, "instance_total_hourly_cost_per_hour"),
		"The total hourly cost of the instance in USD/h",
		[]string{"instance", "region", "family", "machine_type", "price_tier"},
		nil,
	)
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
func (c *Collector) Collect(ch chan<- prometheus.Metric) error {
	now := time.Now()
	c.logger.LogAttrs(c.context, slog.LevelInfo, "Collecting Metrics")
	if c.pricingMap == nil || time.Now().After(c.NextScrape) {
		now := time.Now()
		c.logger.LogAttrs(c.context, slog.LevelInfo, "Generating Pricing Map")
		var prices []string
		var spotPrices []ec2Types.SpotPrice
		eg := new(errgroup.Group)
		eg.SetLimit(5)
		m := sync.Mutex{}
		for _, region := range c.Regions {
			eg.Go(func() error {
				c.logger.LogAttrs(c.context, slog.LevelDebug, "Getting on demand prices for region", slog.String("region", *region.RegionName))
				priceList, err := compute.ListOnDemandPrices(context.TODO(), *region.RegionName, c.pricingService)
				if err != nil {
					return fmt.Errorf("%w: %w", compute.ErrListOnDemandPrices, err)
				}

				if c.ec2RegionClient[*region.RegionName] == nil {
					return ErrClientNotFound
				}
				client := c.ec2RegionClient[*region.RegionName]
				c.logger.LogAttrs(c.context, slog.LevelDebug, "Getting spot prices for region", slog.String("region", *region.RegionName))
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
		c.logger.LogAttrs(c.context, slog.LevelInfo, "Generated Pricing Map",
			slog.Duration("duration", time.Since(now)),
		)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	wg := sync.WaitGroup{}
	wg.Add(len(c.Regions))
	reservationsCh := make(chan []ec2Types.Reservation, len(c.Regions))

	for _, region := range c.Regions {
		go func(region ec2Types.Region) {
			defer wg.Done()
			c.logger.LogAttrs(ctx, slog.LevelInfo, "Fetching instances for region",
				slog.String("region", *region.RegionName),
			)

			if _, ok := c.ec2RegionClient[*region.RegionName]; !ok {
				c.logger.LogAttrs(ctx, slog.LevelError, "could not find client for region",
					slog.String("region", *region.RegionName),
				)
				return
			}
			client := c.ec2RegionClient[*region.RegionName]
			reservations, err := compute.ListComputeInstances(context.TODO(), client)
			if err != nil {
				c.logger.LogAttrs(ctx, slog.LevelError, "could not list instances",
					slog.String("message", err.Error()),
				)
				return
			}
			reservationsCh <- reservations
		}(region)
	}
	go func() {
		wg.Wait()
		close(reservationsCh)
	}()

	c.emitMetrics(reservationsCh, ch)

	c.logger.LogAttrs(c.context, slog.LevelInfo, "Finished Collecting metrics",
		slog.Duration("duration", time.Since(now)),
	)

	return nil
}

func (c *Collector) emitMetrics(reservationsCh chan []ec2Types.Reservation, ch chan<- prometheus.Metric) {
	for reservations := range reservationsCh {
		for _, reservation := range reservations {
			for _, instance := range reservation.Instances {
				clusterName := compute.ClusterNameFromInstance(instance)
				if clusterName != "" {
					c.logger.LogAttrs(c.context, slog.LevelDebug, "filtering out instance that's associated with an eks cluster",
						slog.String("instance", *instance.InstanceId))
					continue
				}
				c.logger.LogAttrs(c.context, slog.LevelDebug, "instance found",
					slog.String("instance", *instance.InstanceId))
				region := *instance.Placement.AvailabilityZone

				pricetier := "spot"
				if instance.InstanceLifecycle != ec2Types.InstanceLifecycleTypeSpot {
					pricetier = "ondemand"
					// Ondemand instances are keyed based upon their region, so we need to remove the availability zone
					region = region[:len(region)-1]
				}
				price, err := c.pricingMap.GetPriceForInstanceType(region, string(instance.InstanceType))
				if err != nil {
					c.logger.LogAttrs(c.context, slog.LevelError, "could not get price for instance type",
						slog.String("instance_type", string(instance.InstanceType)),
						slog.String("region", region),
						slog.String("message", err.Error()),
					)
					continue
				}

				labelValues := []string{
					*instance.PrivateDnsName,
					region,
					c.pricingMap.InstanceDetails[string(instance.InstanceType)].InstanceFamily,
					string(instance.InstanceType),
					pricetier,
				}
				ch <- prometheus.MustNewConstMetric(totalHourlyCostDesc, prometheus.GaugeValue, price.Total, labelValues...)
			}
		}
	}
}

func (c *Collector) Describe(ch chan<- *prometheus.Desc) error {
	c.logger.LogAttrs(c.context, slog.LevelInfo, "Calling describe")
	ch <- totalHourlyCostDesc
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
