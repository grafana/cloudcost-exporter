package gke

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/grafana/cloudcost-exporter/pkg/google/client"
	"github.com/grafana/cloudcost-exporter/pkg/utils"

	"github.com/prometheus/client_golang/prometheus"

	cloudcostexporter "github.com/grafana/cloudcost-exporter"
	"github.com/grafana/cloudcost-exporter/pkg/provider"
)

const (
	subsystem = "gcp_gke"

	// DefaultZoneCollectConcurrency caps the total number of zone-level goroutines
	// (ListInstances + ListDisks) that run simultaneously per project during a
	// scrape. GCP regions contain ~50 zones; without a limit every scrape would
	// fire ~100 concurrent API calls per project. The default of 10 means at most
	// 5 zones are queried in parallel, which stays well within GCP quota defaults.
	// Override via Config.ZoneConcurrency to trade burst rate for scrape latency.
	DefaultZoneCollectConcurrency = 10
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
	// ZoneConcurrency caps zone-level goroutines per project during a scrape.
	// Falls back to DefaultZoneCollectConcurrency.
	ZoneConcurrency int
}

type Collector struct {
	projects   []string
	regions    []string
	pricingMap *PricingMap
	nodeStore  *NodeStore
	diskStore  *DiskStore
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
		// zoneConcurrency caps the total across both, so at most
		// zoneConcurrency/2 zones are queried in parallel.
		zoneConcurrency := c.config.ZoneConcurrency
		if zoneConcurrency <= 0 {
			zoneConcurrency = DefaultZoneCollectConcurrency
		}
		eg, egCtx := errgroup.WithContext(ctx)
		eg.SetLimit(zoneConcurrency)
		instances := make(chan []*client.MachineSpec, len(zones))
		disks := make(chan []*compute.Disk, len(zones))
		for _, zone := range zones {
			eg.Go(func() error {
				now := time.Now()
				c.logger.LogAttrs(egCtx, slog.LevelInfo,
					"Listing instances for project %s in zone %s",
					slog.String("project", project),
					slog.String("zone", zone.Name))

	select {
	case <-c.nodeStore.Done():
		for _, project := range c.projects {
			for _, instance := range c.nodeStore.GetNodes(project) {
				clusterName := instance.GetClusterName()
				if clusterName == "" {
					c.logger.LogAttrs(ctx, slog.LevelDebug, "instance does not have a cluster name",
						slog.String("region", instance.Region),
						slog.String("machine_type", instance.MachineType),
						slog.String("project", project))
					continue
				}
				cpuCost, ramCost, err := c.pricingMap.GetCostOfInstance(instance)
				if err != nil {
					c.logger.LogAttrs(ctx, slog.LevelError, err.Error(),
						slog.String("machine_type", instance.MachineType),
						slog.String("region", instance.Region),
						slog.String("project", project))
					continue
				}
				labelValues := []string{clusterName, instance.Instance, instance.Region, instance.Family, instance.MachineType, project, instance.PriceTier}
				ch <- prometheus.MustNewConstMetric(gkeNodeCPUHourlyCostDesc, prometheus.GaugeValue, cpuCost, labelValues...)
				ch <- prometheus.MustNewConstMetric(gkeNodeMemoryHourlyCostDesc, prometheus.GaugeValue, ramCost, labelValues...)
				nodeCount++
			}
		}
	default:
		c.logger.LogAttrs(ctx, slog.LevelInfo, "node store not yet populated, skipping node metrics")
	}

	select {
	case <-c.diskStore.Done():
		for _, project := range c.projects {
			for _, d := range c.diskStore.GetDisks(project) {
				prices, err := c.pricingMap.GetCostOfStorage(d.Region(), d.StorageClass())
				if err != nil {
					c.logger.LogAttrs(ctx, slog.LevelError, err.Error(),
						slog.String("disk_name", d.name),
						slog.String("project", project),
						slog.String("region", d.Region()),
						slog.String("cluster_name", d.Cluster),
						slog.String("storage_class", d.StorageClass()))
					continue
				}
				ch <- prometheus.MustNewConstMetric(
					persistentVolumeHourlyCostDesc,
					prometheus.GaugeValue,
					computeDiskCost(d, prices),
					d.Cluster, d.Namespace(), d.Name(), d.Region(), d.Project, d.StorageClass(), d.DiskType(), d.UseStatus(),
				)
				diskCount++
			}
		}
	default:
		c.logger.LogAttrs(ctx, slog.LevelInfo, "disk store not yet populated, skipping disk metrics")
	}

	c.logger.LogAttrs(ctx, slog.LevelInfo, "metrics collected",
		slog.Duration("duration", time.Since(now)),
		slog.Int("nodes", nodeCount),
		slog.Int("disks", diskCount))
	return nil
}

func New(ctx context.Context, config *Config, logger *slog.Logger, gcpClient client.Client) (*Collector, error) {
	logger = logger.With("collector", "gke")

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
				if err := pm.Populate(ctx); err != nil {
					logger.Error(err.Error())
				}
			}
		}
	}()

	projects := strings.Split(config.Projects, ",")
	regions := client.RegionsFromZonesForProjects(gcpClient, projects, logger)

	nodeStore := NewNodeStore(ctx, logger, gcpClient, projects)
	diskStore := NewDiskStore(ctx, logger, gcpClient, projects)

	go func() {
		nodeTicker := time.NewTicker(nodeRefreshInterval)
		defer nodeTicker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-nodeTicker.C:
				nodeStore.Populate(ctx)
			}
		}
	}()

	go func() {
		diskTicker := time.NewTicker(diskRefreshInterval)
		defer diskTicker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-diskTicker.C:
				diskStore.Populate(ctx)
			}
		}
	}()

	return &Collector{
		projects:   projects,
		regions:    regions,
		logger:     logger,
		pricingMap: pm,
		nodeStore:  nodeStore,
		diskStore:  diskStore,
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
	ch <- persistentVolumeHourlyCostDesc
	return nil
}
