package rds

import (
	"log/slog"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/rds"
	cloudcost_exporter "github.com/grafana/cloudcost-exporter"
	pricingClient "github.com/grafana/cloudcost-exporter/pkg/aws/services/pricing"
	"github.com/grafana/cloudcost-exporter/pkg/provider"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	subsystem = "aws_rds"

	errGroupLimit = 5
)

type Metrics struct {
	// StorageGauge measures the cost of storage in $/GiB, per region and class.
	DBCost *prometheus.GaugeVec

	// OperationsGauge measures the cost of operations in $/1k requests
	DBOperationsCost *prometheus.GaugeVec

	// RequestCount is a counter that tracks the number of requests made to the AWS Cost Explorer API
	RequestCount prometheus.Counter

	// RequestErrorsCount is a counter that tracks the number of errors when making requests to the AWS Cost Explorer API
	RequestErrorsCount prometheus.Counter

	// NextScrapeGauge is a gauge that tracks the next time the exporter will scrape AWS billing data
	NextScrape prometheus.Gauge
}

func NewMetrics() Metrics {
	return Metrics{
		DBCost: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: prometheus.BuildFQName(cloudcost_exporter.MetricPrefix, subsystem, "storage_by_location_usd_per_gibyte_hour"),
			Help: "Storage cost of RDS databases by region, class, and tier. Cost represented in USD/(GiB*h)",
		},
			[]string{"region", "class"},
		),

		DBOperationsCost: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: prometheus.BuildFQName(cloudcost_exporter.MetricPrefix, subsystem, "operation_by_location_usd_per_krequest"),
			Help: "Operation cost of DB instances by region, class, and tier. Cost represented in USD/(1k req)",
		},
			[]string{"region", "class", "tier"},
		),

		RequestCount: prometheus.NewCounter(prometheus.CounterOpts{
			Name: prometheus.BuildFQName(cloudcost_exporter.ExporterName, subsystem, "cost_api_requests_total"),
			Help: "Total number of requests made to the AWS Cost Explorer API",
		}),

		RequestErrorsCount: prometheus.NewCounter(prometheus.CounterOpts{
			Name: prometheus.BuildFQName(cloudcost_exporter.ExporterName, subsystem, "cost_api_requests_errors_total"),
			Help: "Total number of errors when making requests to the AWS Cost Explorer API",
		}),

		NextScrape: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: prometheus.BuildFQName(cloudcost_exporter.ExporterName, subsystem, "next_scrape"),
			Help: "The next time the exporter will scrape AWS billing data. Can be used to trigger alerts if now - nextScrape > interval",
		}),
	}
}

// Collector is a prometheus collector that collects metrics from AWS EKS clusters.
type Collector struct {
	Regions           []string
	ScrapeInterval    time.Duration
	pricingService    pricingClient.Pricing
	NextComputeScrape time.Time
	NextStorageScrape time.Time
	rdsRegionClients  map[string]rds.Client
	logger            *slog.Logger
}

type Config struct {
	ScrapeInterval time.Duration
	RegionClients  map[string]rds.Client
	Logger         *slog.Logger
}

// New creates an rds collector
func New(config *Config, ps pricingClient.Pricing) *Collector {
	logger := config.Logger.With("logger", "rds")
	return &Collector{
		ScrapeInterval:   config.ScrapeInterval,
		rdsRegionClients: config.RegionClients,
		logger:           logger,
		pricingService:   ps,
	}
}

// CollectMetrics is a no-op function that satisfies the provider.Collector interface.
// Deprecated: CollectMetrics is deprecated and will be removed in a future release.
func (c *Collector) CollectMetrics(_ chan<- prometheus.Metric) float64 {
	return 0
}

// Collect satisfies the provider.Collector interface.
func (c *Collector) Collect(ch chan<- prometheus.Metric) error {
	// start := time.Now()
	// ctx := context.Background()
	// c.logger.LogAttrs(ctx, slog.LevelInfo, "calling collect")

	// // TODO: make both maps scraping run async in the background
	// if c.computePricingMap == nil || time.Now().After(c.NextComputeScrape) {
	// 	err := c.populateComputePricingMap(ctx)
	// 	if err != nil {
	// 		return err
	// 	}
	// 	c.NextComputeScrape = time.Now().Add(c.ScrapeInterval)
	// }

	// if c.storagePricingMap == nil || time.Now().After(c.NextStorageScrape) {
	// 	err := c.populateStoragePricingMap(ctx)
	// 	if err != nil {
	// 		return err
	// 	}
	// 	c.NextStorageScrape = time.Now().Add(c.ScrapeInterval)
	// }

	// numOfRegions := len(c.Regions)

	// wgInstances := sync.WaitGroup{}
	// wgInstances.Add(numOfRegions)
	// instanceCh := make(chan []ec2Types.Reservation, numOfRegions)

	// wgVolumes := sync.WaitGroup{}
	// wgVolumes.Add(numOfRegions)
	// volumeCh := make(chan []ec2Types.Volume, numOfRegions)

	// for _, region := range c.Regions {
	// 	regionName := *region.RegionName
	// 	client := c.ec2RegionClients[regionName]

	// 	if client == nil {
	// 		return ErrClientNotFound
	// 	}

	// 	go func() {
	// 		c.fetchInstancesData(ctx, client, regionName, instanceCh)
	// 		wgInstances.Done()
	// 	}()
	// 	go func() {
	// 		c.fetchVolumesData(ctx, client, regionName, volumeCh)
	// 		wgVolumes.Done()
	// 	}()
	// }
	// go func() {
	// 	wgInstances.Wait()
	// 	close(instanceCh)
	// }()
	// go func() {
	// 	wgVolumes.Wait()
	// 	close(volumeCh)
	// }()
	// c.emitMetricsFromReservationsChannel(instanceCh, ch)
	// c.emitMetricsFromVolumesChannel(volumeCh, ch)
	// c.logger.LogAttrs(ctx, slog.LevelInfo, "Finished collect", slog.Duration("duration", time.Since(start)))
	return nil
}

// func (c *Collector) populateComputePricingMap(errGroupCtx context.Context) error {
// 	c.logger.LogAttrs(errGroupCtx, slog.LevelInfo, "Refreshing compute pricing map")
// 	var prices []string
// 	var spotPrices []ec2Types.SpotPrice
// 	eg, errGroupCtx := errgroup.WithContext(errGroupCtx)
// 	eg.SetLimit(errGroupLimit)
// 	m := sync.Mutex{}
// 	for _, region := range c.Regions {
// 		eg.Go(func() error {
// 			c.logger.LogAttrs(errGroupCtx, slog.LevelDebug, "fetching compute pricing info", slog.String("region", *region.RegionName))

// 			if c.ec2RegionClients[*region.RegionName] == nil {
// 				return ErrClientNotFound
// 			}
// 			client := c.ec2RegionClients[*region.RegionName]
// 			spotPriceList, err := ListSpotPrices(errGroupCtx, client)
// 			if err != nil {
// 				return fmt.Errorf("%w: %w", ErrListSpotPrices, err)
// 			}

// 			priceList, err := ListOnDemandPrices(errGroupCtx, *region.RegionName, c.pricingService)
// 			if err != nil {
// 				return fmt.Errorf("%w: %w", ErrListOnDemandPrices, err)
// 			}

// 			m.Lock()
// 			spotPrices = append(spotPrices, spotPriceList...)
// 			prices = append(prices, priceList...)
// 			m.Unlock()
// 			return nil
// 		})
// 	}
// 	err := eg.Wait()
// 	if err != nil {
// 		return err
// 	}
// 	c.computePricingMap = NewComputePricingMap(c.logger)
// 	if err := c.computePricingMap.GenerateComputePricingMap(prices, spotPrices); err != nil {
// 		return fmt.Errorf("%w: %w", ErrGeneratePricingMap, err)
// 	}

// 	return nil
// }

// func (c *Collector) populateStoragePricingMap(ctx context.Context) error {
// 	c.logger.LogAttrs(ctx, slog.LevelInfo, "Refreshing storage pricing map")
// 	var storagePrices []string
// 	eg, ctx := errgroup.WithContext(ctx)
// 	eg.SetLimit(errGroupLimit)
// 	m := sync.Mutex{}
// 	for _, region := range c.Regions {
// 		eg.Go(func() error {
// 			c.logger.LogAttrs(ctx, slog.LevelDebug, "fetching storage pricing info", slog.String("region", *region.RegionName))
// 			storagePriceList, err := ListStoragePrices(ctx, *region.RegionName, c.pricingService)
// 			if err != nil {
// 				return fmt.Errorf("%w: %w", ErrListStoragePrices, err)
// 			}

// 			m.Lock()
// 			storagePrices = append(storagePrices, storagePriceList...)
// 			m.Unlock()
// 			return nil
// 		})
// 	}
// 	err := eg.Wait()
// 	if err != nil {
// 		return err
// 	}
// 	c.storagePricingMap = NewStoragePricingMap(c.logger)
// 	if err := c.storagePricingMap.GenerateStoragePricingMap(storagePrices); err != nil {
// 		return fmt.Errorf("%w: %w", ErrGeneratePricingMap, err)
// 	}

// 	return nil
// }

// func (c *Collector) fetchInstancesData(ctx context.Context, client ec2client.EC2, region string, instanceCh chan []ec2Types.Reservation) {
// 	now := time.Now()
// 	c.logger.LogAttrs(ctx, slog.LevelInfo, "Fetching instances", slog.String("region", region))

// 	reservations, err := ListComputeInstances(ctx, client)
// 	if err != nil {
// 		c.logger.LogAttrs(ctx, slog.LevelError, "Could not list compute instances",
// 			slog.String("region", region),
// 			slog.String("message", err.Error()))
// 		return
// 	}

// 	c.logger.LogAttrs(ctx, slog.LevelInfo, "Successfully listed instances",
// 		slog.String("region", region),
// 		slog.Int("instances", len(reservations)),
// 		slog.Duration("duration", time.Since(now)),
// 	)

// 	instanceCh <- reservations
// }

// func (c *Collector) fetchVolumesData(ctx context.Context, client ec2client.EC2, region string, volumeCh chan []ec2Types.Volume) {
// 	now := time.Now()
// 	c.logger.LogAttrs(ctx, slog.LevelInfo, "Fetching volumes", slog.String("region", region))

// 	volumes, err := ListEBSVolumes(ctx, client)
// 	if err != nil {
// 		c.logger.LogAttrs(ctx, slog.LevelError, "Could not list EBS volumes",
// 			slog.String("region", region),
// 			slog.String("message", err.Error()))
// 		return
// 	}

// 	c.logger.LogAttrs(ctx, slog.LevelInfo, "Successfully listed volumes",
// 		slog.String("region", region),
// 		slog.Int("volumes", len(volumes)),
// 		slog.Duration("duration", time.Since(now)),
// 	)

// 	volumeCh <- volumes
// }

// func (c *Collector) emitMetricsFromReservationsChannel(reservationsCh chan []ec2Types.Reservation, ch chan<- prometheus.Metric) {
// 	for reservations := range reservationsCh {
// 		for _, reservation := range reservations {
// 			for _, instance := range reservation.Instances {
// 				clusterName := ClusterNameFromInstance(instance)
// 				if instance.PrivateDnsName == nil || *instance.PrivateDnsName == "" {
// 					c.logger.Debug(fmt.Sprintf("no private dns name found for instance %s", *instance.InstanceId))
// 					continue
// 				}
// 				if instance.Placement == nil || instance.Placement.AvailabilityZone == nil {
// 					c.logger.Debug(fmt.Sprintf("no availability zone found for instance %s", *instance.InstanceId))
// 					continue
// 				}

// 				region := *instance.Placement.AvailabilityZone

// 				pricetier := "spot"
// 				if instance.InstanceLifecycle != ec2Types.InstanceLifecycleTypeSpot {
// 					pricetier = "ondemand"
// 					// Ondemand instances are keyed based upon their Region, so we need to remove the availability zone
// 					region = region[:len(region)-1]
// 				}

// 				price, err := c.computePricingMap.GetPriceForInstanceType(region, string(instance.InstanceType))
// 				if err != nil {
// 					c.logger.Error(fmt.Sprintf("error getting price for instance type %s: %s", instance.InstanceType, err))
// 					continue
// 				}

// 				labelValues := []string{
// 					*instance.PrivateDnsName,
// 					*instance.InstanceId,
// 					region,
// 					c.computePricingMap.InstanceDetails[string(instance.InstanceType)].InstanceFamily,
// 					string(instance.InstanceType),
// 					clusterName,
// 					pricetier,
// 					string(instance.Architecture),
// 				}
// 				ch <- prometheus.MustNewConstMetric(InstanceCPUHourlyCostDesc, prometheus.GaugeValue, price.Cpu, labelValues...)
// 				ch <- prometheus.MustNewConstMetric(InstanceMemoryHourlyCostDesc, prometheus.GaugeValue, price.Ram, labelValues...)
// 				ch <- prometheus.MustNewConstMetric(InstanceTotalHourlyCostDesc, prometheus.GaugeValue, price.Total, labelValues...)
// 			}
// 		}
// 	}
// }

// func (c *Collector) emitMetricsFromVolumesChannel(volumesCh chan []ec2Types.Volume, ch chan<- prometheus.Metric) {
// 	for volumes := range volumesCh {
// 		for _, volume := range volumes {
// 			if volume.AvailabilityZone == nil {
// 				c.logger.Error("Volume's Availability Zone unknown: skipping")
// 				continue
// 			}

// 			az := *volume.AvailabilityZone
// 			// Might not be accurate every case, but it's not worth another API call to get the exact region of an AZ
// 			region := az[0 : len(az)-1]

// 			if volume.Size == nil {
// 				c.logger.Error("Volume's size unknown: skipping")
// 				continue
// 			}

// 			price, err := c.storagePricingMap.GetPriceForVolumeType(region, string(volume.VolumeType), *volume.Size)
// 			if err != nil {
// 				c.logger.Error(fmt.Sprintf("error getting price for volume type %s in region %s: %s", volume.VolumeType, region, err))
// 				continue
// 			}

// 			labelValues := []string{
// 				NameFromVolume(volume),
// 				region,
// 				az,
// 				*volume.VolumeId,
// 				string(volume.VolumeType),
// 				strconv.FormatInt(int64(*volume.Size), 10),
// 				string(volume.State),
// 			}
// 			ch <- prometheus.MustNewConstMetric(PersistentVolumeHourlyCostDesc, prometheus.GaugeValue, price, labelValues...)
// 		}
// 	}
// }

func (c *Collector) Describe(ch chan<- *prometheus.Desc) error {
	// ch <- InstanceCPUHourlyCostDesc
	// ch <- InstanceMemoryHourlyCostDesc
	// ch <- InstanceTotalHourlyCostDesc
	// ch <- PersistentVolumeHourlyCostDesc
	return nil
}

func (c *Collector) Name() string {
	return subsystem
}

func (c *Collector) Register(_ provider.Registry) error {
	return nil
}
