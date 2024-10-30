package gke

import (
	"context"
	"errors"
	"fmt"
	"github.com/grafana/cloudcost-exporter/pkg/utils"
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
				cpuCost, ramCost, err := c.PricingMap.GetCostOfInstance(instance)
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

				price, err := c.PricingMap.GetCostOfStorage(d.Region(), d.StorageClass())
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
