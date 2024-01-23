package gke

import (
	"log"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/api/compute/v1"

	gcp_compute "github.com/grafana/cloudcost-exporter/pkg/google/compute"

	cloudcost_exporter "github.com/grafana/cloudcost-exporter"
	"github.com/grafana/cloudcost-exporter/pkg/provider"
)

const (
	subsystem       = "gke"
	gkeClusterLabel = "goog-k8s-cluster-name"
)

var (
	InstanceCPUHourlyCostDesc = prometheus.NewDesc(
		prometheus.BuildFQName(cloudcost_exporter.ExporterName, subsystem, "instance_cpu_hourly_cost"),
		"The hourly cost per CPU core of a GCP GKE Node",
		[]string{"instance", "region", "family", "machine_type", "project", "price_tier", "provider", "cluster_name"},
		nil,
	)
	InstanceMemoryHourlyCostDesc = prometheus.NewDesc(
		prometheus.BuildFQName(cloudcost_exporter.ExporterName, subsystem, "instance_memory_hourly_cost"),
		"The hourly cost per GiB of memory for a GCP GKE Node",
		[]string{"instance", "region", "family", "machine_type", "project", "price_tier", "provider", "cluster_name"},
		nil,
	)
)

type Config struct {
	Projects       string // ProjectID is where the project is running. Used for authentication.
	ScrapeInterval time.Duration
}

type Collector struct {
	computeService   *compute.Service
	config           *Config
	Projects         []string
	computeCollector *gcp_compute.Collector
}

func (c *Collector) Register(_ provider.Registry) error {
	return nil
}

func (c *Collector) CollectMetrics(ch chan<- prometheus.Metric) float64 {
	return 0
}

func (c *Collector) Collect(ch chan<- prometheus.Metric) float64 {
	start := time.Now()
	log.Printf("Collecting %s metrics", c.Name())
	err := c.computeCollector.RefreshPricingMap(start)
	if err != nil {
		return 0
	}
	// ch <- prometheus.MustNewConstMetric(NextScrapeDesc, prometheus.GaugeValue, float64(c.NextScrape.Unix()))
	for _, project := range c.Projects {
		instances, err := gcp_compute.ListInstances(project, c.computeService)
		if err != nil {
			return 0
		}
		for _, instance := range instances {
			cpuCost, ramCost, err := c.computeCollector.PricingMap.GetCostOfInstance(instance)
			if err != nil {
				log.Printf("Could not get cost of instance(%s): %s", instance.Instance, err)
				continue
			}
			ch <- prometheus.MustNewConstMetric(
				InstanceCPUHourlyCostDesc,
				prometheus.GaugeValue,
				cpuCost,
				instance.Instance,
				instance.Region,
				instance.Family,
				instance.MachineType,
				project,
				gcp_compute.PriceTierForInstance(instance),
				"gcp",
				getClusterName(instance.Labels))
			ch <- prometheus.MustNewConstMetric(InstanceMemoryHourlyCostDesc,
				prometheus.GaugeValue,
				ramCost,
				instance.Instance,
				instance.Region,
				instance.Family,
				instance.MachineType,
				project,
				gcp_compute.PriceTierForInstance(instance),
				"gcp",
				getClusterName(instance.Labels))
		}
	}
	log.Printf("Finished collecting GKE metrics in %s", time.Since(start))

	return 1.0
}

func New(config *Config, computeService *compute.Service, computeCollector *gcp_compute.Collector) *Collector {
	projects := strings.Split(config.Projects, ",")
	return &Collector{
		computeService:   computeService,
		config:           config,
		Projects:         projects,
		computeCollector: computeCollector,
	}
}

func (c *Collector) Name() string {
	return "GKE Collector"
}

func (c *Collector) Describe(ch chan<- *prometheus.Desc) error {
	ch <- gkeNodeInfoDesc
	return nil
}

func getClusterName(labels map[string]string) string {
	if clusterName, ok := labels[gkeClusterLabel]; ok {
		return clusterName
	}
	return ""
}
