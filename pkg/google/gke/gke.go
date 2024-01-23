package gke

import (
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/api/compute/v1"

	gcpCompute "github.com/grafana/cloudcost-exporter/pkg/google/compute"

	cloudcostexporter "github.com/grafana/cloudcost-exporter"
	"github.com/grafana/cloudcost-exporter/pkg/provider"
)

const (
	subsystem = "gke"
)

var (
	gkeNodeInfoDesc = prometheus.NewDesc(
		prometheus.BuildFQName(cloudcostexporter.ExporterName, subsystem, "node_info"),
		"Information about GKE nodes",
		[]string{"project", "instance_name", "cluster_name"},
		nil,
	)
	gkeNodeCPUHoulryCostDesc = prometheus.NewDesc(
		prometheus.BuildFQName(cloudcostexporter.ExporterName, subsystem, "node_cpu_hourly_cost"),
		"Hourly cost of a GKE node",
		[]string{"project", "instance_name", "cluster_name"},
		nil,
	)
)

type Config struct {
	Projects       string // ProjectID is where the project is running. Used for authentication.
	ScrapeInterval time.Duration
}

type Collector struct {
	computeService *compute.Service
	config         *Config
	Projects       []string
}

func (c *Collector) Register(_ provider.Registry) error {
	return nil
}

func (c *Collector) CollectMetrics(ch chan<- prometheus.Metric) float64 {
	return 0
}

func (c *Collector) Collect(ch chan<- prometheus.Metric) error {
	for _, project := range c.Projects {
		instances, err := gcpCompute.ListInstances(project, c.computeService)
		if err != nil {
			return err
		}
		for _, instance := range instances {
			if instance.ClusterName == "" {
				continue
			}
			ch <- prometheus.MustNewConstMetric(
				gkeNodeInfoDesc,
				prometheus.GaugeValue,
				1,
				project,
				instance.Instance,
				instance.ClusterName,
			)
		}
	}
	return nil
}

func New(config *Config, computeService *compute.Service) *Collector {
	projects := strings.Split(config.Projects, ",")
	return &Collector{
		computeService: computeService,
		config:         config,
		Projects:       projects,
	}
}

func (c *Collector) Name() string {
	return "GKE Collector"
}

func (c *Collector) Describe(ch chan<- *prometheus.Desc) error {
	ch <- gkeNodeInfoDesc
	ch <- gkeNodeCPUHoulryCostDesc
	return nil
}
