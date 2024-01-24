package compute

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	billingv1 "cloud.google.com/go/billing/apiv1"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/api/compute/v1"

	cloudcost_exporter "github.com/grafana/cloudcost-exporter"
	"github.com/grafana/cloudcost-exporter/pkg/google/billing"
	"github.com/grafana/cloudcost-exporter/pkg/provider"
)

const (
	subsystem = "gcp_compute"
)

var (
	ListInstancesError = errors.New("no list price was found for the sku")
)

var (
	NextScrapeDesc = prometheus.NewDesc(
		prometheus.BuildFQName(cloudcost_exporter.ExporterName, subsystem, "next_scrape"),
		"Next time GCP's compute submodule pricing map will be refreshed as unix timestamp",
		nil,
		nil,
	)
	InstanceCPUHourlyCostDesc = prometheus.NewDesc(
		prometheus.BuildFQName(cloudcost_exporter.MetricPrefix, subsystem, "instance_cpu_usd_per_core_hour"),
		"The cpu cost a GCP Compute Instance in USD/(core*h)",
		[]string{"instance", "region", "family", "machine_type", "project", "price_tier"},
		nil,
	)
	InstanceMemoryHourlyCostDesc = prometheus.NewDesc(
		prometheus.BuildFQName(cloudcost_exporter.MetricPrefix, subsystem, "instance_ram_usd_per_gib_hour"),
		"The memory cost of a GCP Compute Instance in USD/(GiB*h)",
		[]string{"instance", "region", "family", "machine_type", "project", "price_tier"},
		nil,
	)
)

type Config struct {
	Projects       string
	ScrapeInterval time.Duration
}

// Collector implements the Collector interface for compute services in Compute.
type Collector struct {
	computeService *compute.Service
	billingService *billingv1.CloudCatalogClient
	PricingMap     *billing.StructuredPricingMap
	config         *Config
	Projects       []string
	NextScrape     time.Time
}

func (c *Collector) Describe(ch chan<- *prometheus.Desc) error {
	ch <- NextScrapeDesc
	ch <- InstanceCPUHourlyCostDesc
	ch <- InstanceMemoryHourlyCostDesc
	return nil
}

func (c *Collector) Collect(ch chan<- prometheus.Metric) error {
	up := c.CollectMetrics(ch)
	if up == 0 {
		return fmt.Errorf("error collecting metrics")
	}
	return nil
}

// New is a helper method to properly set up a compute.Collector struct.
func New(config *Config, computeService *compute.Service, billingService *billingv1.CloudCatalogClient) *Collector {
	projects := strings.Split(config.Projects, ",")
	return &Collector{
		computeService: computeService,
		billingService: billingService,
		config:         config,
		Projects:       projects,
	}
}

// Name returns a well formatted string for the name of the collector. Helpful for logging
func (c *Collector) Name() string {
	return "Compute Collector"
}

// ListInstances will collect all the node instances that are running within a GCP project.
func ListInstances(projectID string, c *compute.Service) ([]*billing.MachineSpec, error) {
	var allInstances []*billing.MachineSpec
	var nextPageToken string
	log.Printf("Listing instances for project %s", projectID)
	for {
		instances, err := c.Instances.AggregatedList(projectID).
			PageToken(nextPageToken).
			Do()
		if err != nil {
			log.Printf("Error listing instance templates: %s", err)
			return nil, fmt.Errorf("%w: %s", ListInstancesError, err.Error())
		}
		for _, instanceList := range instances.Items {
			for _, instance := range instanceList.Instances {
				allInstances = append(allInstances, billing.NewMachineSpec(instance))
			}
		}
		nextPageToken = instances.NextPageToken
		if nextPageToken == "" {
			break
		}
	}
	return allInstances, nil
}

func (c *Collector) Register(registry provider.Registry) error {
	log.Printf("Registering %s", c.Name())
	return nil
}

func (c *Collector) CollectMetrics(ch chan<- prometheus.Metric) float64 {
	start := time.Now()
	log.Printf("Collecting %s metrics", c.Name())
	ctx := context.TODO()
	if c.PricingMap == nil || time.Now().After(c.NextScrape) {
		log.Println("Refreshing pricing map")
		serviceName, err := billing.GetServiceName(ctx, c.billingService)
		if err != nil {
			log.Printf("Error getting service name: %s", err)
			return 0
		}
		skus := billing.GetPricing(ctx, c.billingService, serviceName)
		pricingMap, err := billing.GeneratePricingMap(skus)
		if err != nil {
			log.Printf("Error generating pricing map: %s", err)
			return 0
		}

		c.PricingMap = pricingMap
		c.NextScrape = time.Now().Add(c.config.ScrapeInterval)
		log.Printf("Finished refreshing pricing map in %s", time.Since(start))
	}
	ch <- prometheus.MustNewConstMetric(NextScrapeDesc, prometheus.GaugeValue, float64(c.NextScrape.Unix()))
	for _, project := range c.Projects {
		instances, err := ListInstances(project, c.computeService)
		if err != nil {
			return 0
		}
		for _, instance := range instances {
			cpuCost, ramCost, err := c.PricingMap.GetCostOfInstance(instance)
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
				instance.PriceTier)
			ch <- prometheus.MustNewConstMetric(InstanceMemoryHourlyCostDesc,
				prometheus.GaugeValue,
				ramCost,
				instance.Instance,
				instance.Region,
				instance.Family,
				instance.MachineType,
				project,
				instance.PriceTier)
		}
	}
	log.Printf("Finished collecting Compute metrics in %s", time.Since(start))

	return 1.0
}
