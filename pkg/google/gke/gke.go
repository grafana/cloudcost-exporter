package gke

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	billingv1 "cloud.google.com/go/billing/apiv1"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/api/compute/v1"

	"github.com/grafana/cloudcost-exporter/pkg/google/billing"
	gcpCompute "github.com/grafana/cloudcost-exporter/pkg/google/compute"

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
		prometheus.BuildFQName(cloudcostexporter.MetricPrefix, subsystem, "persistent_volume_usd_per_gib_hour"),
		"The cost of a GKE Persistent Volume in USD/(GiB*h)",
		[]string{"cluster_name", "persistentvolume", "region", "project", "storage_class"},
		nil,
	)
)

type Config struct {
	Projects       string
	ScrapeInterval time.Duration
}

type Collector struct {
	computeService *compute.Service
	billingService *billingv1.CloudCatalogClient
	config         *Config
	Projects       []string
	PricingMap     *gcpCompute.StructuredPricingMap
	NextScrape     time.Time
}

func (c *Collector) Register(_ provider.Registry) error {
	return nil
}

func (c *Collector) CollectMetrics(ch chan<- prometheus.Metric) float64 {
	err := c.Collect(ch)
	if err != nil {
		log.Printf("failed to collect metrics: %v", err)
		return 0
	}
	return 1
}

func (c *Collector) Collect(ch chan<- prometheus.Metric) error {
	ctx := context.TODO()
	if c.PricingMap == nil || time.Now().After(c.NextScrape) {
		serviceName, err := billing.GetServiceName(ctx, c.billingService, "Compute Engine")
		if err != nil {
			return err
		}
		skus := billing.GetPricing(ctx, c.billingService, serviceName)
		c.PricingMap, err = gcpCompute.GeneratePricingMap(skus)
		if err != nil {
			return err
		}
		c.NextScrape = time.Now().Add(c.config.ScrapeInterval)
	}

	for _, project := range c.Projects {
		zones, err := c.computeService.Zones.List(project).Do()
		if err != nil {
			return err
		}
		wg := sync.WaitGroup{}
		// Multiply by 2 because we are making two requests per zone
		wg.Add(len(zones.Items) * 2)
		instances := make(chan []*gcpCompute.MachineSpec, len(zones.Items))
		disks := make(chan []*compute.Disk, len(zones.Items))
		for _, zone := range zones.Items {
			go func(zone *compute.Zone) {
				defer wg.Done()
				results, err := gcpCompute.ListInstancesInZone(project, zone.Name, c.computeService)
				if err != nil {
					log.Printf("error listing instances in zone %s: %v", zone.Name, err)
					instances <- nil
					return
				}
				instances <- results
			}(zone)
			go func(zone *compute.Zone) {
				defer wg.Done()
				results, err := ListDisks(project, zone.Name, c.computeService)
				if err != nil {
					log.Printf("error listing disks in zone %s: %v", zone.Name, err)
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
		for group := range disks {
			for _, disk := range group {
				clusterName := disk.Labels[gcpCompute.GkeClusterLabel]
				region := disk.Labels["google-k8s-cluster-location"]
				diskType := strings.Split(disk.Type, "/")
				storageClass := diskType[len(diskType)-1]
				labelValues := []string{
					clusterName,
					disk.Name,
					region,
					project,
					storageClass,
				}
				ch <- prometheus.MustNewConstMetric(
					persistentVolumeHourlyCostDesc,
					prometheus.GaugeValue,
					float64(disk.SizeGb)*0.05/720,
					labelValues...,
				)
			}
		}
	}
	return nil
}

func New(config *Config, computeService *compute.Service, billingService *billingv1.CloudCatalogClient) *Collector {
	projects := strings.Split(config.Projects, ",")
	return &Collector{
		computeService: computeService,
		billingService: billingService,
		config:         config,
		Projects:       projects,
	}
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
	disks := []*compute.Disk{}
	err := service.Disks.List(project, zone).Pages(context.Background(), func(page *compute.DiskList) error {
		for _, disk := range page.Items {
			// We only care about in use disks
			if len(disk.Users) == 0 {
				continue
			}
			// Filter out disks that are not associated with a GKE cluster
			if disk.Labels[gcpCompute.GkeClusterLabel] == "" {
				continue
			}
			disks = append(disks, disk)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return disks, nil
}
