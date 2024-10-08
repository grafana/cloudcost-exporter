package gke

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	billingv1 "cloud.google.com/go/billing/apiv1"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/api/compute/v1"

	cloudcostexporter "github.com/grafana/cloudcost-exporter"
	"github.com/grafana/cloudcost-exporter/pkg/provider"
)

const (
	subsystem = "gcp_gke"
)

var (
	gkeNodeMemoryHourlyCostDesc = prometheus.NewDesc(
		prometheus.BuildFQName(cloudcostexporter.MetricPrefix, subsystem, "instance_memory_usd_per_gib_hour"),

		"The cpu cost a GKE Instance in USD/(core*h)",
		// Cannot simply do cluster because many metric scrapers will add a label for cluster and would interfere with the label we want to add
		[]string{"cluster_name", "instance", "region", "family", "machine_type", "project", "price_tier"},
		nil,
	)
	gkeNodeCPUHourlyCostDesc = prometheus.NewDesc(
		prometheus.BuildFQName(cloudcostexporter.MetricPrefix, subsystem, "instance_cpu_usd_per_core_hour"),
		"The memory cost of a GKE Instance in USD/(GiB*h)",
		// Cannot simply do cluster because many metric scrapers will add a label for cluster and would interfere with the label we want to add
		[]string{"cluster_name", "instance", "region", "family", "machine_type", "project", "price_tier"},
		nil,
	)
	persistentVolumeHourlyCostDesc = prometheus.NewDesc(
		prometheus.BuildFQName(cloudcostexporter.MetricPrefix, subsystem, "persistent_volume_usd_per_hour"),
		"The cost of a GKE Persistent Volume in USD.",
		[]string{"cluster_name", "namespace", "persistentvolume", "region", "project", "storage_class", "disk_type"},
		nil,
	)
)

var (
	ErrListInstances = errors.New("no list price was found for the sku")
)

type Config struct {
	Projects       string
	ScrapeInterval time.Duration
	Logger         *slog.Logger
}

type Collector struct {
	computeService *compute.Service
	billingService *billingv1.CloudCatalogClient
	config         *Config
	Projects       []string
	PricingMap     *PricingMap
	NextScrape     time.Time
	logger         *slog.Logger
}

func (c *Collector) Register(_ provider.Registry) error {
	return nil
}

func (c *Collector) CheckReadiness() bool {
	// TODO - implement
	return true
}

func (c *Collector) CollectMetrics(ch chan<- prometheus.Metric) float64 {
	err := c.Collect(ch)
	if err != nil {
		c.logger.Error("failed to collect metrics", slog.String("msg", err.Error()))
		return 0
	}
	return 1
}

func (c *Collector) Collect(ch chan<- prometheus.Metric) error {
	ctx := context.Background()
	for _, project := range c.Projects {
		zones, err := c.computeService.Zones.List(project).Do()
		if err != nil {
			return err
		}
		wg := sync.WaitGroup{}
		// Multiply by 2 because we are making two requests per zone
		wg.Add(len(zones.Items) * 2)
		instances := make(chan []*MachineSpec, len(zones.Items))
		disks := make(chan []*compute.Disk, len(zones.Items))
		for _, zone := range zones.Items {
			go func(zone *compute.Zone) {
				defer wg.Done()
				now := time.Now()
				c.logger.LogAttrs(ctx, slog.LevelInfo,
					"Listing instances for project %s in zone %s",
					slog.String("project", project),
					slog.String("zone", zone.Name))

				results, err := ListInstancesInZone(project, zone.Name, c.computeService)
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
				results, err := ListDisks(project, zone.Name, c.computeService)
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
				cpuCost, ramCost, err := c.PricingMap.GetCostOfInstance(instance)
				if err != nil {
					return err
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
				}

				price, err := c.PricingMap.GetCostOfStorage(d.Region(), d.StorageClass())
				if err != nil {
					fmt.Printf("%s error getting cost of storage: %v\n", disk.Name, err)
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

func New(config *Config, computeService *compute.Service, billingService *billingv1.CloudCatalogClient) (*Collector, error) {
	logger := config.Logger.With("collector", "gke")
	ctx := context.TODO()

	pm, err := NewPricingMap(ctx, billingService)
	if err != nil {
		return nil, err
	}

	priceTicker := time.NewTicker(PriceRefreshInterval)

	go func(ctx context.Context, billingService *billingv1.CloudCatalogClient) {
		for {
			select {
			case <-ctx.Done():
				return
			case <-priceTicker.C:
				err := pm.Populate(ctx, billingService)
				if err != nil {
					logger.Error(err.Error())
				}
			}
		}
	}(ctx, billingService)

	return &Collector{
		computeService: computeService,
		billingService: billingService,
		config:         config,
		Projects:       strings.Split(config.Projects, ","),
		logger:         logger,
		PricingMap:     pm,
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

// ListDisks will list all disks in a given zone and return a slice of compute.Disk
func ListDisks(project string, zone string, service *compute.Service) ([]*compute.Disk, error) {
	var disks []*compute.Disk
	// TODO: How do we get this to work for multi regional disks?
	err := service.Disks.List(project, zone).Pages(context.Background(), func(page *compute.DiskList) error {
		if page == nil {
			return nil
		}
		disks = append(disks, page.Items...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return disks, nil
}

// ListInstancesInZone will list all instances in a given zone and return a slice of MachineSpecs
func ListInstancesInZone(projectID, zone string, c *compute.Service) ([]*MachineSpec, error) {
	var allInstances []*MachineSpec
	var nextPageToken string

	for {
		instances, err := c.Instances.List(projectID, zone).
			PageToken(nextPageToken).
			Do()
		if err != nil {
			return nil, fmt.Errorf("%w: %s", ErrListInstances, err.Error())
		}
		for _, instance := range instances.Items {
			allInstances = append(allInstances, NewMachineSpec(instance))
		}
		nextPageToken = instances.NextPageToken
		if nextPageToken == "" {
			break
		}
	}
	return allInstances, nil
}
