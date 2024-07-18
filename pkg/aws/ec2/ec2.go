package ec2

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"strconv"
	"sync"
	"time"

	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/sync/errgroup"

	cloudcostexporter "github.com/grafana/cloudcost-exporter"
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
	InstanceCPUHourlyCostDesc = prometheus.NewDesc(
		prometheus.BuildFQName(cloudcostexporter.MetricPrefix, subsystem, "instance_cpu_usd_per_core_hour"),
		"The cpu cost a ec2 instance in USD/(core*h)",
		[]string{"instance", "region", "family", "machine_type", "cluster_name", "price_tier"},
		nil,
	)
	InstanceMemoryHourlyCostDesc = prometheus.NewDesc(
		prometheus.BuildFQName(cloudcostexporter.MetricPrefix, subsystem, "instance_memory_usd_per_gib_hour"),
		"The memory cost of a ec2 instance in USD/(GiB*h)",
		[]string{"instance", "region", "family", "machine_type", "cluster_name", "price_tier"},
		nil,
	)
	InstanceTotalHourlyCostDesc = prometheus.NewDesc(
		prometheus.BuildFQName(cloudcostexporter.MetricPrefix, subsystem, "instance_total_usd_per_hour"),
		"The total cost of the ec2 instance in USD/h",
		[]string{"instance", "region", "family", "machine_type", "cluster_name", "price_tier"},
		nil,
	)
	persistentVolumeHourlyCostDesc = prometheus.NewDesc(
		prometheus.BuildFQName(cloudcostexporter.MetricPrefix, subsystem, "persistent_volume_usd_per_hour"),
		"The cost of an AWS EBS Volume in USD.",
		[]string{"persistent_volume", "region", "availability_zone", "disk", "type", "size_gib", "state"},
		nil,
	)
)

// Collector is a prometheus collector that collects metrics from AWS EKS clusters.
type Collector struct {
	Regions                 []ec2Types.Region
	ScrapeInterval          time.Duration
	computePricingMap       *ComputePricingMap
	storagePricingMap       *StoragePricingMap
	pricingService          pricingClient.Pricing
	ComputeScrapingInterval time.Time
	StorageScrapingInterval time.Time
	ec2RegionClients        map[string]ec2client.EC2
	logger                  *slog.Logger
}

type Config struct {
	ScrapeInterval time.Duration
	Regions        []ec2Types.Region
	RegionClients  map[string]ec2client.EC2
	Logger         *slog.Logger
}

// New creates an ec2 collector
func New(config *Config, ps pricingClient.Pricing) *Collector {
	logger := config.Logger.With("logger", "ec2")
	return &Collector{
		ScrapeInterval:    config.ScrapeInterval,
		Regions:           config.Regions,
		ec2RegionClients:  config.RegionClients,
		logger:            logger,
		pricingService:    ps,
		computePricingMap: NewComputePricingMap(),
		storagePricingMap: NewStoragePricingMap(),
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

	// TODO: make both maps scraping run async in the background
	if c.computePricingMap == nil || time.Now().After(c.ComputeScrapingInterval) {
		err := c.populateComputePricingMap()
		if err != nil {
			return err
		}
	}

	if c.storagePricingMap == nil || time.Now().After(c.StorageScrapingInterval) {
		err := c.populateStoragePricingMap()
		if err != nil {
			return err
		}
	}

	wgInstances := sync.WaitGroup{}
	wgInstances.Add(len(c.Regions))
	instanceCh := make(chan []ec2Types.Reservation, len(c.Regions))

	wgVolumes := sync.WaitGroup{}
	wgVolumes.Add(len(c.Regions))
	volumeCh := make(chan []ec2Types.Volume, len(c.Regions))

	for _, region := range c.Regions {
		regionName := *region.RegionName
		client := c.ec2RegionClients[regionName]

		go c.fetchInstancesData(region, &wgInstances, instanceCh)
		go c.fetchVolumesData(client, regionName, &wgVolumes, volumeCh)
	}
	go func() {
		wgInstances.Wait()
		close(instanceCh)
	}()
	go func() {
		wgVolumes.Wait()
		close(volumeCh)
	}()
	c.emitMetricsFromReservationsChannel(instanceCh, ch)
	c.emitMetricsFromVolumesChannel(volumeCh, ch)
	c.logger.LogAttrs(context.TODO(), slog.LevelInfo, "Finished collect", slog.Duration("duration", time.Since(start)))
	return nil
}

func (c *Collector) populateComputePricingMap() error {
	c.logger.LogAttrs(context.Background(), slog.LevelInfo, "Refreshing compute pricing map")
	var prices []string
	var spotPrices []ec2Types.SpotPrice
	eg := new(errgroup.Group)
	eg.SetLimit(5)
	m := sync.Mutex{}
	for _, region := range c.Regions {
		eg.Go(func() error {
			ctx := context.Background()
			c.logger.LogAttrs(ctx, slog.LevelDebug, "fetching compute pricing info", slog.String("region", *region.RegionName))
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
	c.computePricingMap = NewComputePricingMap()
	if err := c.computePricingMap.GenerateComputePricingMap(prices, spotPrices); err != nil {
		return fmt.Errorf("%w: %w", ErrGeneratePricingMap, err)
	}
	c.ComputeScrapingInterval = time.Now().Add(c.ScrapeInterval)
	return nil
}

func (c *Collector) populateStoragePricingMap() error {
	c.logger.LogAttrs(context.Background(), slog.LevelInfo, "Refreshing storage pricing map")
	var storagePrices []string
	eg := new(errgroup.Group)
	eg.SetLimit(5)
	m := sync.Mutex{}
	for _, region := range c.Regions {
		eg.Go(func() error {
			ctx := context.Background()
			c.logger.LogAttrs(ctx, slog.LevelDebug, "fetching storage pricing info", slog.String("region", *region.RegionName))
			storagePriceList, err := ListStoragePrices(context.Background(), *region.RegionName, c.pricingService)
			if err != nil {
				return fmt.Errorf("%w: %w", ErrListStoragePrices, err)
			}

			m.Lock()
			storagePrices = append(storagePrices, storagePriceList...)
			m.Unlock()
			return nil
		})
	}
	err := eg.Wait()
	if err != nil {
		return err
	}
	c.storagePricingMap = NewStoragePricingMap()
	if err := c.storagePricingMap.GenerateStoragePricingMap(storagePrices); err != nil {
		return fmt.Errorf("%w: %w", ErrGeneratePricingMap, err)
	}
	c.StorageScrapingInterval = time.Now().Add(c.ScrapeInterval)
	return nil
}

func (c *Collector) fetchInstancesData(region ec2Types.Region, wg *sync.WaitGroup, instanceCh chan []ec2Types.Reservation) {
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
}

func (c *Collector) fetchVolumesData(client ec2client.EC2, region string, wg *sync.WaitGroup, volumeCh chan []ec2Types.Volume) {
	ctx := context.Background()
	now := time.Now()
	c.logger.LogAttrs(ctx, slog.LevelInfo, "Fetching volumes", slog.String("region", region))
	defer wg.Done()

	volumes, err := ListEBSVolumes(context.Background(), client)
	if err != nil {
		c.logger.LogAttrs(ctx, slog.LevelError, "Could not list EBS volumes",
			slog.String("region", region),
			slog.String("message", err.Error()))
		return
	}
	c.logger.LogAttrs(ctx, slog.LevelInfo, "Successfully listed volumes",
		slog.String("region", region),
		slog.Int("volumes", len(volumes)),
		slog.Duration("duration", time.Since(now)),
	)
	volumeCh <- volumes
}

func (c *Collector) emitMetricsFromReservationsChannel(reservationsCh chan []ec2Types.Reservation, ch chan<- prometheus.Metric) {
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
				price, err := c.computePricingMap.GetPriceForInstanceType(region, string(instance.InstanceType))
				if err != nil {
					log.Printf("error getting price for instance type %s: %s", instance.InstanceType, err)
					continue
				}
				labelValues := []string{
					*instance.PrivateDnsName,
					region,
					c.computePricingMap.InstanceDetails[string(instance.InstanceType)].InstanceFamily,
					string(instance.InstanceType),
					clusterName,
					pricetier,
				}
				ch <- prometheus.MustNewConstMetric(InstanceCPUHourlyCostDesc, prometheus.GaugeValue, price.Cpu, labelValues...)
				ch <- prometheus.MustNewConstMetric(InstanceMemoryHourlyCostDesc, prometheus.GaugeValue, price.Ram, labelValues...)
				ch <- prometheus.MustNewConstMetric(InstanceTotalHourlyCostDesc, prometheus.GaugeValue, price.Total, labelValues...)
			}
		}
	}
}

func (c *Collector) emitMetricsFromVolumesChannel(volumesCh chan []ec2Types.Volume, ch chan<- prometheus.Metric) {
	for volumes := range volumesCh {
		for _, volume := range volumes {
			az := *volume.AvailabilityZone
			// Might not be accurate every case, but it's not worth another API call to get the exact region of an AZ
			region := az[0 : len(az)-1]

			price, err := c.storagePricingMap.GetPriceForVolumeType(region, string(volume.VolumeType), *volume.Size)
			if err != nil {
				log.Printf("error getting price for volume type %s in region %s: %s", volume.VolumeType, region, err)
				continue
			}

			labelValues := []string{
				NameFromVolume(volume),
				region,
				az,
				*volume.VolumeId,
				string(volume.VolumeType),
				strconv.FormatInt(int64(*volume.Size), 10),
				string(volume.State),
			}

			ch <- prometheus.MustNewConstMetric(persistentVolumeHourlyCostDesc, prometheus.GaugeValue, price, labelValues...)
		}
	}
}

func (c *Collector) CheckReadiness() bool {
	// TODO add storagePricingMap to the readiness check
	return c.computePricingMap.CheckReadiness()
}

func (c *Collector) Describe(ch chan<- *prometheus.Desc) error {
	ch <- InstanceCPUHourlyCostDesc
	ch <- InstanceMemoryHourlyCostDesc
	ch <- InstanceTotalHourlyCostDesc
	ch <- persistentVolumeHourlyCostDesc
	return nil
}

func (c *Collector) Name() string {
	return subsystem
}

func (c *Collector) Register(_ provider.Registry) error {
	return nil
}
