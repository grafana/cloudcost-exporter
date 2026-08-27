package gce

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/grafana/cloudcost-exporter/pkg/google/client"
	"github.com/grafana/cloudcost-exporter/pkg/google/gke"
	"github.com/grafana/cloudcost-exporter/pkg/google/storeutil"
	"github.com/grafana/cloudcost-exporter/pkg/utils"

	"github.com/prometheus/client_golang/prometheus"

	cloudcostexporter "github.com/grafana/cloudcost-exporter"
	"github.com/grafana/cloudcost-exporter/pkg/provider"
)

const (
	subsystem = "gcp_gce"

	// nodeRefreshInterval mirrors gke's own node-store refresh cadence. gke's
	// equivalent constant is unexported, so it's duplicated here rather than
	// widening gke's exported surface just for this.
	nodeRefreshInterval = 5 * time.Minute
)

var (
	gceInstanceCPUHourlyCostDesc = utils.GenerateDesc(
		cloudcostexporter.MetricPrefix,
		subsystem,
		utils.InstanceCPUCostSuffix,
		"The CPU cost of a standalone GCE Instance in USD/(core*h)",
		[]string{"instance", "region", "family", "machine_type", "project", "price_tier"},
	)
	gceInstanceMemoryHourlyCostDesc = utils.GenerateDesc(
		cloudcostexporter.MetricPrefix,
		subsystem,
		utils.InstanceMemoryCostSuffix,
		"The memory cost of a standalone GCE Instance in USD/(GiB*h)",
		[]string{"instance", "region", "family", "machine_type", "project", "price_tier"},
	)
	gceInstanceTotalHourlyCostDesc = utils.GenerateDesc(
		cloudcostexporter.MetricPrefix,
		subsystem,
		utils.InstanceTotalCostSuffix,
		"The total cost of a standalone GCE Instance in USD/h. Absent for an instance whose machine type spec hasn't been resolved yet.",
		[]string{"instance", "region", "family", "machine_type", "project", "price_tier"},
	)
)

type Config struct {
	Projects       string
	ScrapeInterval time.Duration
	// ZoneConcurrency caps zone-level goroutines per project during a scrape.
	// Falls back to gke.DefaultZoneCollectConcurrency.
	ZoneConcurrency int
}

type Collector struct {
	projects       []string
	regions        []string
	pricingMap     *gke.PricingMap
	nodeStore      *gke.NodeStore
	machineTypes   *machineTypeCache
	logger         *slog.Logger
	populateErrors *prometheus.CounterVec
}

func (c *Collector) Register(r provider.Registry) error {
	r.MustRegister(c.populateErrors)
	return nil
}

func (c *Collector) Collect(ctx context.Context, ch chan<- prometheus.Metric) error {
	now := time.Now()
	var instanceCount int64

	select {
	case <-c.nodeStore.Done():
		for _, project := range c.projects {
			for _, instance := range c.nodeStore.GetNodes(project) {
				if instance.GetClusterName() != "" {
					// GKE-managed node: priced by the gcp_gke collector instead, so cost is
					// never double-counted between gcp_gke_* and gcp_gce_* metrics.
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
				labelValues := []string{instance.Instance, instance.Region, instance.Family, instance.MachineType, project, instance.PriceTier}
				ch <- prometheus.MustNewConstMetric(gceInstanceCPUHourlyCostDesc, prometheus.GaugeValue, cpuCost, labelValues...)
				ch <- prometheus.MustNewConstMetric(gceInstanceMemoryHourlyCostDesc, prometheus.GaugeValue, ramCost, labelValues...)

				if spec, ok := c.machineTypes.get(project, instance.Zone, instance.MachineType); ok {
					total := cpuCost*float64(spec.VCPU) + ramCost*spec.MemoryGiB
					ch <- prometheus.MustNewConstMetric(gceInstanceTotalHourlyCostDesc, prometheus.GaugeValue, total, labelValues...)
				} else {
					c.logger.LogAttrs(ctx, slog.LevelDebug, "machine type spec not cached, skipping total cost metric",
						slog.String("machine_type", instance.MachineType),
						slog.String("zone", instance.Zone),
						slog.String("project", project))
				}
				instanceCount++
			}
		}
	default:
		c.logger.LogAttrs(ctx, slog.LevelInfo, "node store not yet populated, skipping instance metrics")
	}

	c.logger.LogAttrs(ctx, slog.LevelInfo, "metrics collected",
		slog.Duration("duration", time.Since(now)),
		slog.Int64("instances_emitted", instanceCount))
	return nil
}

func New(ctx context.Context, config *Config, logger *slog.Logger, gcpClient client.Client) (*Collector, error) {
	logger = logger.With("collector", "gce")

	pm, err := gke.NewPricingMap(ctx, gcpClient)
	if err != nil {
		return nil, err
	}

	projects := strings.Split(config.Projects, ",")
	regions := client.RegionsFromZonesForProjects(gcpClient, projects, logger)

	populateErrors := storeutil.NewPopulateErrorsCounter(subsystem)
	nodeStore := gke.NewNodeStore(ctx, logger, gcpClient, projects, config.ZoneConcurrency, populateErrors)
	machineTypes := newMachineTypeCache(gcpClient, populateErrors, config.ZoneConcurrency, logger)

	storeutil.StartRefreshTicker(ctx, gke.PriceRefreshInterval, func() {
		if err := pm.Populate(ctx); err != nil {
			logger.Error(err.Error())
		}
	})
	storeutil.StartRefreshTicker(ctx, nodeRefreshInterval, func() {
		nodeStore.Populate(ctx)
		machineTypes.warm(ctx, nodeStore, projects)
	})
	// Warm the machine-type cache right after the node store's own initial populate
	// completes, rather than waiting for the first nodeRefreshInterval tick.
	go func() {
		select {
		case <-nodeStore.Done():
			machineTypes.warm(ctx, nodeStore, projects)
		case <-ctx.Done():
		}
	}()

	return &Collector{
		projects:       projects,
		regions:        regions,
		logger:         logger,
		pricingMap:     pm,
		nodeStore:      nodeStore,
		machineTypes:   machineTypes,
		populateErrors: populateErrors,
	}, nil
}

func (c *Collector) Regions() []string {
	return c.regions
}

func (c *Collector) Name() string {
	return subsystem
}

func (c *Collector) Describe(ch chan<- *prometheus.Desc) error {
	ch <- gceInstanceCPUHourlyCostDesc
	ch <- gceInstanceMemoryHourlyCostDesc
	ch <- gceInstanceTotalHourlyCostDesc
	return nil
}
