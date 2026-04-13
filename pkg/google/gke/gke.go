package gke

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/grafana/cloudcost-exporter/pkg/google/client"
	"github.com/grafana/cloudcost-exporter/pkg/utils"
	"golang.org/x/sync/errgroup"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/api/compute/v1"

	cloudcostexporter "github.com/grafana/cloudcost-exporter"
	"github.com/grafana/cloudcost-exporter/pkg/provider"
)

const (
	subsystem = "gcp_gke"

	// zoneCollectConcurrencyLimit caps the total number of zone-level goroutines
	// (ListInstances + ListDisks) that run simultaneously per project during a
	// scrape. GCP regions contain ~50 zones; without a limit every scrape would
	// fire ~100 concurrent API calls per project. The value of 10 means at most
	// 5 zones are queried in parallel, which stays well within GCP quota defaults.
	zoneCollectConcurrencyLimit = 10
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
	regions    []string
	pricingMap *PricingMap
	logger     *slog.Logger
}

func (c *Collector) Register(_ provider.Registry) error {
	return nil
}

func (c *Collector) Collect(ctx context.Context, ch chan<- prometheus.Metric) error {
	for _, project := range c.projects {
		zones, err := c.gcpClient.GetZones(project)
		if err != nil {
			return err
		}
		// Two goroutines are launched per zone (ListInstances + ListDisks).
		// zoneCollectConcurrencyLimit caps the total across both, so at most
		// zoneCollectConcurrencyLimit/2 zones are queried in parallel.
		eg, egCtx := errgroup.WithContext(ctx)
		eg.SetLimit(zoneCollectConcurrencyLimit)
		instances := make(chan []*client.MachineSpec, len(zones))
		disks := make(chan []*compute.Disk, len(zones))
		for _, zone := range zones {
			eg.Go(func() error {
				now := time.Now()
				c.logger.LogAttrs(egCtx, slog.LevelInfo,
					"Listing instances for project %s in zone %s",
					slog.String("project", project),
					slog.String("zone", zone.Name))

				results, err := c.gcpClient.ListInstancesInZone(project, zone.Name)
				if err != nil {
					c.logger.LogAttrs(egCtx, slog.LevelError,
						"error listing instances in zone",
						slog.String("project", project),
						slog.String("zone", zone.Name),
						slog.Duration("duration", time.Since(now)),
						slog.String("msg", err.Error()))

					instances <- nil
					return nil
				}
				c.logger.LogAttrs(egCtx, slog.LevelInfo,
					"finished listing instances in zone",
					slog.String("project", project),
					slog.String("zone", zone.Name),
					slog.Duration("duration", time.Since(now)))
				instances <- results
				return nil
			})
			eg.Go(func() error {
				results, err := c.gcpClient.ListDisks(egCtx, project, zone.Name)
				if err != nil {
					c.logger.Error("error listing disks in zone %s: %v",
						slog.String("zone", zone.Name),
						slog.String("msg", err.Error()))
					return nil
				}
				disks <- results
				return nil
			})
		}

		go func() {
			// Goroutines always return nil; Wait() will not return an error.
			_ = eg.Wait()
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

				prices, err := c.pricingMap.GetCostOfStorage(d.Region(), d.StorageClass())
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
					computeDiskCost(d, prices),
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

	go func() {
		priceTicker := time.NewTicker(PriceRefreshInterval)
		defer priceTicker.Stop()
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

	projects := strings.Split(config.Projects, ",")
	regions := client.RegionsFromZonesForProjects(gcpClient, projects, logger)

	return &Collector{
		config:     config,
		projects:   projects,
		regions:    regions,
		logger:     logger,
		pricingMap: pm,
		gcpClient:  gcpClient,
	}, nil
}

func (c *Collector) Regions() []string {
	return c.regions
}

func (c *Collector) Name() string {
	return subsystem
}

func (c *Collector) Describe(ch chan<- *prometheus.Desc) error {
	ch <- gkeNodeCPUHourlyCostDesc
	ch <- gkeNodeMemoryHourlyCostDesc
	return nil
}
