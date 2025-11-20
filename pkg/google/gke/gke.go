package gke

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/grafana/cloudcost-exporter/pkg/google/client"
	"github.com/grafana/cloudcost-exporter/pkg/utils"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/api/compute/v1"

	cloudcostexporter "github.com/grafana/cloudcost-exporter"
	"github.com/grafana/cloudcost-exporter/pkg/provider"
)

const (
	subsystem = "gcp_gke"
)

var (
	gkeNodeMemoryHourlyCostDesc = utils.GenerateDesc(
		cloudcostexporter.MetricPrefix,
		subsystem,
		utils.InstanceMemoryCostSuffix,
		"The memory cost of a GKE Instance in USD/(GiB*h)",
		// Cannot simply use "cluster" because other metric scrapers may add a label for cluster, which would interfere
		[]string{"cluster_name", "instance", "region", "family", "machine_type", "project", "price_tier"},
	)
	gkeNodeCPUHourlyCostDesc = utils.GenerateDesc(
		cloudcostexporter.MetricPrefix,
		subsystem,
		utils.InstanceCPUCostSuffix,
		"The CPU cost of a GKE Instance in USD/(core*h)",
		// Cannot simply use "cluster" because other metric scrapers may add a label for cluster, which would interfere
		[]string{"cluster_name", "instance", "region", "family", "machine_type", "project", "price_tier"},
	)
	persistentVolumeHourlyCostDesc = utils.GenerateDesc(
		cloudcostexporter.MetricPrefix,
		subsystem,
		utils.PersistentVolumeCostSuffix,
		"The cost of a GKE Persistent Volume in USD/h",
		[]string{"cluster_name", "namespace", "persistentvolume", "region", "project", "storage_class", "disk_type", "use_status"},
	)
)

type Config struct {
	Projects       string
	ScrapeInterval time.Duration
	Logger         *slog.Logger
}

type Collector struct {
	gcpClient  client.Client
	config     *Config
	projects   []string
	pricingMap *PricingMap
	logger     *slog.Logger
}

func (c *Collector) Register(_ provider.Registry) error {
	return nil
}

func (c *Collector) CollectMetrics(ch chan<- prometheus.Metric) float64 {
	err := c.Collect(context.Background(), ch)
	if err != nil {
		c.logger.Error("failed to collect metrics", slog.String("msg", err.Error()))
		return 0
	}
	return 1
}

func (c *Collector) Collect(ctx context.Context, ch chan<- prometheus.Metric) error {
	for _, project := range c.projects {
		zones, err := c.gcpClient.GetZones(project)
		if err != nil {
			return err
		}
		wg := sync.WaitGroup{}
		// Multiply by 2 because we are making two requests per zone
		wg.Add(len(zones) * 2)
		instances := make(chan []*client.MachineSpec, len(zones))
		disks := make(chan []*compute.Disk, len(zones))
		for _, zone := range zones {
			go func(zone *compute.Zone) {
				defer wg.Done()
				now := time.Now()
				c.logger.LogAttrs(ctx, slog.LevelInfo,
					"Listing instances for project %s in zone %s",
					slog.String("project", project),
					slog.String("zone", zone.Name))

				results, err := c.gcpClient.ListInstancesInZone(project, zone.Name)
				if err != nil {
					c.logger.LogAttrs(ctx, slog.LevelError,
						"error listing instances in zone",
						slog.String("project", project),
						slog.String("zone", zone.Name),
						slog.Duration("duration", time.Since(now)),
						slog.String("msg", err.Error()))

					instances <- nil
					return
				}
				c.logger.LogAttrs(ctx, slog.LevelInfo,
					"finished listing instances in zone",
					slog.String("project", project),
					slog.String("zone", zone.Name),
					slog.Duration("duration", time.Since(now)))
				instances <- results
			}(zone)
			go func(zone *compute.Zone) {
				defer wg.Done()
				results, err := c.gcpClient.ListDisks(ctx, project, zone.Name)
				if err != nil {
					c.logger.Error("error listing disks in zone %s: %v",
						slog.String("zone", zone.Name),
						slog.String("msg", err.Error()))
					return
				}
				disks <- results
			}(zone)
		}

		go func() {
			wg.Wait()
			close(instances)
			close(disks)
		}()

		for group := range instances {
			if instances == nil {
				continue
			}
			for _, instance := range group {
				clusterName := instance.GetClusterName()
				// We skip instances that do not have a clusterName because they are not associated with an GKE cluster
				if clusterName == "" {
					c.logger.LogAttrs(ctx,
						slog.LevelDebug,
						"instance does not have a clustername",
						slog.String("region", instance.Region),
						slog.String("machine_type", instance.MachineType),
						slog.String("project", project),
					)
					continue
				}
				labelValues := []string{
					clusterName,
					instance.Instance,
					instance.Region,
					instance.Family,
					instance.MachineType,
					project,
					instance.PriceTier,
				}
				cpuCost, ramCost, err := c.pricingMap.GetCostOfInstance(instance)
				if err != nil {
					// Log out the error and continue processing nodes
					// TODO(@pokom): Should we set sane defaults here to emit _something_?
					c.logger.LogAttrs(ctx,
						slog.LevelError,
						err.Error(),
						slog.String("machine_type", instance.MachineType),
						slog.String("region", instance.Region),
						slog.String("project", project),
					)
					continue
				}
				ch <- prometheus.MustNewConstMetric(
					gkeNodeCPUHourlyCostDesc,
					prometheus.GaugeValue,
					cpuCost,
					labelValues...,
				)
				ch <- prometheus.MustNewConstMetric(
					gkeNodeMemoryHourlyCostDesc,
					prometheus.GaugeValue,
					ramCost,
					labelValues...,
				)
			}
		}
		seenDisks := make(map[string]bool)
		for group := range disks {
			for _, disk := range group {
				d := NewDisk(disk, project)
				// This an effort to deduplicate disks that have duplicate names
				// See https://github.com/grafana/cloudcost-exporter/issues/143
				if _, ok := seenDisks[d.Name()]; ok {
					continue
				}
				seenDisks[d.Name()] = true

				labelValues := []string{
					d.Cluster,
					d.Namespace(),
					d.Name(),
					d.Region(),
					d.Project,
					d.StorageClass(),
					d.DiskType(),
					d.UseStatus(),
				}

				price, err := c.pricingMap.GetCostOfStorage(d.Region(), d.StorageClass())
				if err != nil {
					c.logger.LogAttrs(ctx,
						slog.LevelError,
						err.Error(),
						slog.String("disk_name", disk.Name),
						slog.String("project", project),
						slog.String("region", d.Region()),
						slog.String("cluster_name", d.Cluster),
						slog.String("storage_class", d.StorageClass()),
					)
					continue
				}
				ch <- prometheus.MustNewConstMetric(
					persistentVolumeHourlyCostDesc,
					prometheus.GaugeValue,
					float64(d.Size)*price,
					labelValues...,
				)
			}
		}
	}
	return nil
}

func New(ctx context.Context, config *Config, gcpClient client.Client) (*Collector, error) {
	logger := config.Logger.With("collector", "gke")

	pm, err := NewPricingMap(ctx, gcpClient)
	if err != nil {
		return nil, err
	}

	priceTicker := time.NewTicker(PriceRefreshInterval)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-priceTicker.C:
				err := pm.Populate(ctx)
				if err != nil {
					logger.Error(err.Error())
				}
			}
		}
	}()

	return &Collector{
		config:     config,
		projects:   strings.Split(config.Projects, ","),
		logger:     logger,
		pricingMap: pm,
		gcpClient:  gcpClient,
	}, nil
}

func (c *Collector) Name() string {
	return subsystem
}

func (c *Collector) Describe(ch chan<- *prometheus.Desc) error {
	ch <- gkeNodeCPUHourlyCostDesc
	ch <- gkeNodeMemoryHourlyCostDesc
	return nil
}
