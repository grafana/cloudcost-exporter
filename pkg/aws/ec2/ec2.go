package ec2

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"sync"
	"time"

	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/sync/errgroup"

	cloudcostexporter "github.com/grafana/cloudcost-exporter"
	"github.com/grafana/cloudcost-exporter/cmd/exporter/config"
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

type collectorMetrics struct {
	InstanceCPUHourlyCostDesc    *prometheus.Desc
	InstanceMemoryHourlyCostDesc *prometheus.Desc
	InstanceTotalHourlyCostDesc  *prometheus.Desc
}

// Collector is a prometheus collector that collects metrics from AWS EKS clusters.
type Collector struct {
	Regions          []ec2Types.Region
	ScrapeInterval   time.Duration
	pricingMap       *StructuredPricingMap
	pricingService   pricingClient.Pricing
	NextScrape       time.Time
	ec2RegionClients map[string]ec2client.EC2
	metrics          *collectorMetrics
	logger           *slog.Logger
}

type Config struct {
	CommonConfig   *config.CommonConfig
	ScrapeInterval time.Duration
	Regions        []ec2Types.Region
	RegionClients  map[string]ec2client.EC2
	Logger         *slog.Logger
}

func newCollectorMetrics(instanceLabel string) *collectorMetrics {
	return &collectorMetrics{
		InstanceCPUHourlyCostDesc: prometheus.NewDesc(
			prometheus.BuildFQName(cloudcostexporter.MetricPrefix, subsystem, "instance_cpu_usd_per_core_hour"),
			"The cpu cost a ec2 instance in USD/(core*h)",
			[]string{instanceLabel, "region", "family", "machine_type", "cluster_name", "price_tier"},
			nil,
		),
		InstanceMemoryHourlyCostDesc: prometheus.NewDesc(
			prometheus.BuildFQName(cloudcostexporter.MetricPrefix, subsystem, "instance_memory_usd_per_gib_hour"),
			"The memory cost of a ec2 instance in USD/(GiB*h)",
			[]string{instanceLabel, "region", "family", "machine_type", "cluster_name", "price_tier"},
			nil,
		),
		InstanceTotalHourlyCostDesc: prometheus.NewDesc(
			prometheus.BuildFQName(cloudcostexporter.MetricPrefix, subsystem, "instance_total_usd_per_hour"),
			"The total cost of the ec2 instance in USD/h",
			[]string{instanceLabel, "region", "family", "machine_type", "cluster_name", "price_tier"},
			nil,
		),
	}
}

// New creates an ec2 collector
func New(config *Config, ps pricingClient.Pricing) *Collector {
	logger := config.Logger.With("logger", "ec2")
	return &Collector{
		ScrapeInterval:   config.ScrapeInterval,
		Regions:          config.Regions,
		ec2RegionClients: config.RegionClients,
		metrics:          newCollectorMetrics(config.CommonConfig.ComputeInstanceLabel),
		logger:           logger,
		pricingService:   ps,
		pricingMap:       NewStructuredPricingMap(),
	}
}

// CollectMetrics is a no-op function that satisfies the provider.Collector interface.
// Deprecated: CollectMetrics is deprecated and will be removed in a future release.
func (c *Collector) CollectMetrics(_ chan<- prometheus.Metric) float64 {
	return 0
}

// Collect satisfies the provider.Collector interface.
func (c *Collector) Collect(ch chan<- prometheus.Metric) error {
	start := time.Now()
	c.logger.LogAttrs(context.TODO(), slog.LevelInfo, "calling collect")
	if c.pricingMap == nil || time.Now().After(c.NextScrape) {
		c.logger.LogAttrs(context.Background(), slog.LevelInfo, "Refreshing pricing map")
		var prices []string
		var spotPrices []ec2Types.SpotPrice
		eg := new(errgroup.Group)
		eg.SetLimit(5)
		m := sync.Mutex{}
		for _, region := range c.Regions {
			eg.Go(func() error {
				ctx := context.Background()
				c.logger.LogAttrs(ctx, slog.LevelDebug, "fetching pricing info", slog.String("region", *region.RegionName))
				priceList, err := ListOnDemandPrices(context.Background(), *region.RegionName, c.pricingService)
				if err != nil {
					return fmt.Errorf("%w: %w", ErrListOnDemandPrices, err)
				}

				if c.ec2RegionClients[*region.RegionName] == nil {
					return ErrClientNotFound
				}
				client := c.ec2RegionClients[*region.RegionName]
				spotPriceList, err := ListSpotPrices(context.Background(), client)
				if err != nil {
					return fmt.Errorf("%w: %w", ErrListSpotPrices, err)
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
		c.pricingMap = NewStructuredPricingMap()
		if err := c.pricingMap.GeneratePricingMap(prices, spotPrices); err != nil {
			return fmt.Errorf("%w: %w", ErrGeneratePricingMap, err)
		}
		c.NextScrape = time.Now().Add(c.ScrapeInterval)
	}

	wg := sync.WaitGroup{}
	wg.Add(len(c.Regions))
	instanceCh := make(chan []ec2Types.Reservation, len(c.Regions))
	for _, region := range c.Regions {
		go func(region ec2Types.Region) {
			ctx := context.Background()
			now := time.Now()
			c.logger.LogAttrs(ctx, slog.LevelInfo, "Fetching instances", slog.String("region", *region.RegionName))
			defer wg.Done()
			client := c.ec2RegionClients[*region.RegionName]
			reservations, err := ListComputeInstances(context.Background(), client)
			if err != nil {
				c.logger.LogAttrs(ctx, slog.LevelError, "Could not list compute instances",
					slog.String("region", *region.RegionName),
					slog.String("message", err.Error()))
				return
			}
			c.logger.LogAttrs(ctx, slog.LevelInfo, "Successfully listed instances",
				slog.String("region", *region.RegionName),
				slog.Int("instances", len(reservations)),
				slog.Duration("duration", time.Since(now)),
			)
			instanceCh <- reservations
		}(region)
	}
	go func() {
		wg.Wait()
		close(instanceCh)
	}()
	c.emitMetricsFromChannel(instanceCh, ch)
	c.logger.LogAttrs(context.TODO(), slog.LevelInfo, "Finished collect", slog.Duration("duration", time.Since(start)))
	return nil
}

func (c *Collector) emitMetricsFromChannel(reservationsCh chan []ec2Types.Reservation, ch chan<- prometheus.Metric) {
	for reservations := range reservationsCh {
		for _, reservation := range reservations {
			for _, instance := range reservation.Instances {
				clusterName := ClusterNameFromInstance(instance)
				if instance.PrivateDnsName == nil || *instance.PrivateDnsName == "" {
					log.Printf("no private dns name found for instance %s", *instance.InstanceId)
					continue
				}
				if instance.Placement == nil || instance.Placement.AvailabilityZone == nil {
					log.Printf("no availability zone found for instance %s", *instance.InstanceId)
					continue
				}

				region := *instance.Placement.AvailabilityZone

				pricetier := "spot"
				if instance.InstanceLifecycle != ec2Types.InstanceLifecycleTypeSpot {
					pricetier = "ondemand"
					// Ondemand instances are keyed based upon their Region, so we need to remove the availability zone
					region = region[:len(region)-1]
				}
				price, err := c.pricingMap.GetPriceForInstanceType(region, string(instance.InstanceType))
				if err != nil {
					log.Printf("error getting price for instance type %s: %s", instance.InstanceType, err)
					continue
				}
				labelValues := []string{
					*instance.PrivateDnsName,
					region,
					c.pricingMap.InstanceDetails[string(instance.InstanceType)].InstanceFamily,
					string(instance.InstanceType),
					clusterName,
					pricetier,
				}
				ch <- prometheus.MustNewConstMetric(c.metrics.InstanceCPUHourlyCostDesc, prometheus.GaugeValue, price.Cpu, labelValues...)
				ch <- prometheus.MustNewConstMetric(c.metrics.InstanceMemoryHourlyCostDesc, prometheus.GaugeValue, price.Ram, labelValues...)
				ch <- prometheus.MustNewConstMetric(c.metrics.InstanceTotalHourlyCostDesc, prometheus.GaugeValue, price.Total, labelValues...)
			}
		}
	}
}

func (c *Collector) CheckReadiness() bool {
	return c.pricingMap.CheckReadiness()
}

func (c *Collector) Describe(ch chan<- *prometheus.Desc) error {
	ch <- c.metrics.InstanceCPUHourlyCostDesc
	ch <- c.metrics.InstanceMemoryHourlyCostDesc
	ch <- c.metrics.InstanceTotalHourlyCostDesc
	return nil
}

func (c *Collector) Name() string {
	return subsystem
}

func (c *Collector) Register(_ provider.Registry) error {
	return nil
}
