package ec2

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/grafana/cloudcost-exporter/pkg/aws/client"
	"github.com/grafana/cloudcost-exporter/pkg/utils"

	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/prometheus/client_golang/prometheus"

	cloudcostexporter "github.com/grafana/cloudcost-exporter"
	"github.com/grafana/cloudcost-exporter/pkg/provider"
)

const (
	subsystem = "aws_ec2"

	errGroupLimit = 5
)

var (
	ErrGeneratePricingMap = errors.New("error generating pricing map")
	ErrClientNotFound     = errors.New("no client found")
)

var (
	InstanceCPUHourlyCostDesc = utils.GenerateDesc(
		cloudcostexporter.MetricPrefix,
		subsystem,
		utils.InstanceCPUCostSuffix,
		"The cpu cost a ec2 instance in USD/(core*h)",
		[]string{"instance", "instance_id", "region", "family", "machine_type", "cluster_name", "price_tier", "architecture"},
	)
	InstanceMemoryHourlyCostDesc = utils.GenerateDesc(
		cloudcostexporter.MetricPrefix,
		subsystem,
		utils.InstanceMemoryCostSuffix,
		"The memory cost of a ec2 instance in USD/(GiB*h)",
		[]string{"instance", "instance_id", "region", "family", "machine_type", "cluster_name", "price_tier", "architecture"},
	)
	InstanceTotalHourlyCostDesc = utils.GenerateDesc(
		cloudcostexporter.MetricPrefix,
		subsystem,
		utils.InstanceTotalCostSuffix,
		"The total cost of the ec2 instance in USD/h",
		[]string{"instance", "instance_id", "region", "family", "machine_type", "cluster_name", "price_tier", "architecture"},
	)
	PersistentVolumeHourlyCostDesc = utils.GenerateDesc(
		cloudcostexporter.MetricPrefix,
		subsystem,
		utils.PersistentVolumeCostSuffix,
		"The cost of an AWS EBS Volume in USD/h.",
		[]string{"persistentvolume", "region", "availability_zone", "disk", "type", "size_gib", "state"},
	)
)

// Collector is a prometheus collector that collects metrics from AWS EKS clusters.
type Collector struct {
	Regions            []ec2Types.Region
	ScrapeInterval     time.Duration
	computePricingMap  *ComputePricingMap
	storagePricingMap  *StoragePricingMap
	awsRegionClientMap map[string]client.Client
	logger             *slog.Logger
}

type Config struct {
	ScrapeInterval time.Duration
	Regions        []ec2Types.Region
	Logger         *slog.Logger
	RegionMap      map[string]client.Client
}

// New creates an ec2 collector
func New(ctx context.Context, config *Config) (*Collector, error) {
	logger := config.Logger.With("logger", "ec2")
	computeMap := NewComputePricingMap(logger, config)
	storageMap := NewStoragePricingMap(logger, config)

	computeTicker := time.NewTicker(config.ScrapeInterval)
	storageTicker := time.NewTicker(config.ScrapeInterval)

	// Initial population so that Collect can use the maps
	if err := computeMap.GenerateComputePricingMap(ctx); err != nil {
		return nil, fmt.Errorf("failed initial compute pricing: %w", err)
	}
	if err := storageMap.GenerateStoragePricingMap(ctx); err != nil {
		return nil, fmt.Errorf("failed initial storage pricing: %w", err)
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-computeTicker.C:
				if err := computeMap.GenerateComputePricingMap(ctx); err != nil {
					logger.Error("failed to refresh compute pricing map", "error", err)
				}
			}
		}
	}()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-storageTicker.C:
				if err := storageMap.GenerateStoragePricingMap(ctx); err != nil {
					logger.Error("failed to refresh storage pricing map", "error", err)
				}
			}
		}
	}()

	return &Collector{
		ScrapeInterval:     config.ScrapeInterval,
		Regions:            config.Regions,
		logger:             logger,
		awsRegionClientMap: config.RegionMap,
		computePricingMap:  computeMap,
		storagePricingMap:  storageMap,
	}, nil
}

// CollectMetrics is a no-op function that satisfies the provider.Collector interface.
// Deprecated: CollectMetrics is deprecated and will be removed in a future release.
func (c *Collector) CollectMetrics(_ chan<- prometheus.Metric) float64 {
	return 0
}

// Collect satisfies the provider.Collector interface.
func (c *Collector) Collect(ctx context.Context, ch chan<- prometheus.Metric) error {
	c.logger.LogAttrs(ctx, slog.LevelInfo, "calling collect")

	numOfRegions := len(c.Regions)

	wgInstances := sync.WaitGroup{}
	wgInstances.Add(numOfRegions)
	instanceCh := make(chan []ec2Types.Reservation, numOfRegions)

	wgVolumes := sync.WaitGroup{}
	wgVolumes.Add(numOfRegions)
	volumeCh := make(chan []ec2Types.Volume, numOfRegions)

	for _, region := range c.Regions {
		regionName := *region.RegionName

		regionClient, ok := c.awsRegionClientMap[regionName]
		if !ok {
			return ErrClientNotFound
		}

		go func() {
			c.fetchInstancesData(ctx, regionClient, regionName, instanceCh)
			wgInstances.Done()
		}()
		go func() {
			c.fetchVolumesData(ctx, regionClient, regionName, volumeCh)
			wgVolumes.Done()
		}()
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
	return nil
}

func (c *Collector) fetchInstancesData(ctx context.Context, regionClient client.Client, region string, instanceCh chan []ec2Types.Reservation) {
	now := time.Now()
	c.logger.LogAttrs(ctx, slog.LevelInfo, "Fetching instances", slog.String("region", region))

	reservations, err := regionClient.ListComputeInstances(ctx)
	if err != nil {
		c.logger.LogAttrs(ctx, slog.LevelError, "Could not list compute instances",
			slog.String("region", region),
			slog.String("message", err.Error()))
		return
	}

	c.logger.LogAttrs(ctx, slog.LevelInfo, "Successfully listed instances",
		slog.String("region", region),
		slog.Int("instances", len(reservations)),
		slog.Duration("duration", time.Since(now)),
	)

	instanceCh <- reservations
}

func (c *Collector) fetchVolumesData(ctx context.Context, regionClient client.Client, region string, volumeCh chan []ec2Types.Volume) {
	now := time.Now()
	c.logger.LogAttrs(ctx, slog.LevelInfo, "Fetching volumes", slog.String("region", region))

	volumes, err := regionClient.ListEBSVolumes(ctx)
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
				clusterName := client.ClusterNameFromInstance(instance)
				if instance.PrivateDnsName == nil || *instance.PrivateDnsName == "" {
					c.logger.Debug(fmt.Sprintf("no private dns name found for instance %s", *instance.InstanceId))
					continue
				}
				if instance.Placement == nil || instance.Placement.AvailabilityZone == nil {
					c.logger.Debug(fmt.Sprintf("no availability zone found for instance %s", *instance.InstanceId))
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
					c.logger.Error(fmt.Sprintf("error getting price for instance type %s: %s", instance.InstanceType, err))
					continue
				}

				labelValues := []string{
					*instance.PrivateDnsName,
					*instance.InstanceId,
					region,
					c.computePricingMap.InstanceDetails[string(instance.InstanceType)].InstanceFamily,
					string(instance.InstanceType),
					clusterName,
					pricetier,
					string(instance.Architecture),
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
			if volume.AvailabilityZone == nil {
				c.logger.Error("Volume's Availability Zone unknown: skipping")
				continue
			}

			az := *volume.AvailabilityZone
			// Might not be accurate every case, but it's not worth another API call to get the exact region of an AZ
			region := az[0 : len(az)-1]

			if volume.Size == nil {
				c.logger.Error("Volume's size unknown: skipping")
				continue
			}

			price, err := c.storagePricingMap.GetPriceForVolumeType(region, string(volume.VolumeType), *volume.Size)
			if err != nil {
				c.logger.Error(fmt.Sprintf("error getting price for volume type %s in region %s: %s", volume.VolumeType, region, err))
				continue
			}

			labelValues := []string{
				client.NameFromVolume(volume),
				region,
				az,
				*volume.VolumeId,
				string(volume.VolumeType),
				strconv.FormatInt(int64(*volume.Size), 10),
				string(volume.State),
			}
			ch <- prometheus.MustNewConstMetric(PersistentVolumeHourlyCostDesc, prometheus.GaugeValue, price, labelValues...)
		}
	}
}

func (c *Collector) Describe(ch chan<- *prometheus.Desc) error {
	ch <- InstanceCPUHourlyCostDesc
	ch <- InstanceMemoryHourlyCostDesc
	ch <- InstanceTotalHourlyCostDesc
	ch <- PersistentVolumeHourlyCostDesc
	return nil
}

func (c *Collector) Name() string {
	return subsystem
}

func (c *Collector) Register(_ provider.Registry) error {
	return nil
}
