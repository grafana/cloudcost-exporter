package compute

import (
	"context"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"cloud.google.com/go/billing/apiv1/billingpb"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/api/iterator"

	billingv1 "cloud.google.com/go/billing/apiv1"
	"google.golang.org/api/compute/v1"

	"github.com/grafana/cloudcost-exporter/pkg/provider"
)

var (
	ServiceNotFound    = errors.New("the service for compute engine wasn't found")
	SkuNotFound        = errors.New("no sku was interested in us")
	ListInstancesError = errors.New("no list price was found for the sku")
	re                 = regexp.MustCompile(`\bin\b`)
)

var (
	InstanceCPUHourlyCost = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "instance_cpu_hourly_cost",
		Help: "The hourly cost of a GKE instance",
	}, []string{"instance", "region", "family", "machine_type", "project", "price_tier", "provider"})
	InstanceMemoryHourlyCost = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "instance_memory_hourly_cost",
		Help: "The hourly cost of a GKE instance",
	}, []string{"instance", "region", "family", "machine_type", "project", "price_tier", "provider"})
)

type Config struct {
	Projects       string
	ScrapeInterval time.Duration
}

// Collector implements the Collector interface for compute services in GKE.
type Collector struct {
	computeService *compute.Service
	billingService *billingv1.CloudCatalogClient
	PricingMap     *StructuredPricingMap
	config         *Config
	Projects       []string
	NextScrape     time.Time
}

// New is a helper method to properly setup a compute.Collector struct.
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
	return "GKE Collector"
}

// MachineSpec is a slimmed down representation of a google compute.Instance struct
type MachineSpec struct {
	Instance     string
	Zone         string
	Region       string
	Family       string
	MachineType  string
	SpotInstance bool
}

// NewMachineSpec will create a new MachineSpec from compute.Instance objects.
// It's responsible for determining the machine family and region that it operates in
func NewMachineSpec(instance *compute.Instance) *MachineSpec {
	zone := instance.Zone[strings.LastIndex(instance.Zone, "/")+1:]
	region := getRegionFromZone(zone)
	machineType := getMachineTypeFromURL(instance.MachineType)
	family := getMachineFamily(machineType)
	spot := isSpotInstance(instance.Scheduling.ProvisioningModel)

	return &MachineSpec{
		Instance:     instance.Name,
		Zone:         zone,
		Region:       region,
		MachineType:  machineType,
		Family:       family,
		SpotInstance: spot,
	}
}

func isSpotInstance(model string) bool {
	return model == "SPOT"
}

func getRegionFromZone(zone string) string {
	return zone[:strings.LastIndex(zone, "-")]
}

// ListInstances will collect all of the node instances that are running within a GCP project.
func (c *Collector) ListInstances(projectID string) ([]*MachineSpec, error) {
	var allInstances []*MachineSpec
	var nextPageToken string
	log.Printf("Listing instances for project %s", projectID)
	for {
		instances, err := c.computeService.Instances.AggregatedList(projectID).
			PageToken(nextPageToken).
			Do()
		if err != nil {
			log.Printf("Error listing instance templates: %s", err)
			return nil, fmt.Errorf("%w: %s", ListInstancesError, err.Error())
		}
		for _, instanceList := range instances.Items {
			for _, instance := range instanceList.Instances {
				allInstances = append(allInstances, NewMachineSpec(instance))
			}
		}
		nextPageToken = instances.NextPageToken
		if nextPageToken == "" {
			break
		}
	}
	return allInstances, nil
}

func (c *Collector) GetServiceName() (string, error) {
	serviceIterator := c.billingService.ListServices(context.Background(), &billingpb.ListServicesRequest{PageSize: 5000})
	for {
		service, err := serviceIterator.Next()
		if err != nil {
			if errors.Is(err, iterator.Done) {
				break
			}
			return "", err
		}
		if service.DisplayName == "Compute Engine" {
			return service.Name, nil
		}
	}
	return "", ServiceNotFound
}

// GetPricing will collect all the pricing information for a given service and return a list of skus.
func (c *Collector) GetPricing(serviceName string) []*billingpb.Sku {
	var skus []*billingpb.Sku
	skuIterator := c.billingService.ListSkus(context.Background(), &billingpb.ListSkusRequest{Parent: serviceName})
	for {
		sku, err := skuIterator.Next()
		if err != nil {
			if errors.Is(err, iterator.Done) {
				break
			}
		}
		// We don't include licensing skus in our pricing map
		if !strings.Contains(strings.ToLower(sku.Description), "licensing") {
			skus = append(skus, sku)
		}
	}
	return skus
}

func getMachineTypeFromURL(url string) string {
	return url[strings.LastIndex(url, "/")+1:]
}

func getMachineFamily(machineType string) string {
	if !strings.Contains(machineType, "-") {
		log.Printf("Machine type %s doesn't contain a -", machineType)
		return ""
	}
	split := strings.Split(machineType, "-")
	return strings.ToLower(split[0])
}

func getPricingInfoFromSku(sku *billingpb.Sku) (int32, error) {
	if len(sku.PricingInfo) == 0 {
		return 0, fmt.Errorf("no pricing info found for sku %s", sku.Name)
	}
	pricingInfo := sku.PricingInfo[0]
	if pricingInfo.PricingExpression.TieredRates == nil || len(pricingInfo.PricingExpression.TieredRates) < 1 {
		return 0, fmt.Errorf("no tiered rates found for sku %s", sku.Name)
	}
	return pricingInfo.PricingExpression.TieredRates[0].UnitPrice.Nanos, nil
}

func stripOutKeyFromDescription(description string) string {
	// Except for commitments, the description will have running in it
	runningInIndex := strings.Index(description, "running in")

	if runningInIndex > 0 {
		description = description[:runningInIndex]
		return strings.Trim(description, " ")
	}
	// If we can't find running in, try to find Commitment v1:
	splitString := strings.Split(description, "Commitment v1:")
	if len(splitString) == 1 {
		log.Printf("No running in or commitment found in description: %s", description)
		return ""
	}
	// Take everything after the Commitment v1
	// TODO: Evaluate if we want to consider leaving in Commitment V1
	split := splitString[1]
	// Now something a bit more tricky, we need to find an exact match of "in"
	// Turns out that locations such as Berlin break this assumption
	// SO we need to use a regexp to find the first instance of "in"
	foundIndex := re.FindStringIndex(split)
	if len(foundIndex) == 0 {
		log.Printf("No in found in description: %s", description)
		return ""
	}
	str := split[:foundIndex[0]]
	return strings.Trim(str, " ")
}

func (c *Collector) Register(registry provider.Registry) error {
	log.Println("Registering GKE metrics")
	err := registry.Register(InstanceCPUHourlyCost)
	if err != nil {
		return err
	}
	return registry.Register(InstanceMemoryHourlyCost)
}

func (c *Collector) Collect() float64 {
	start := time.Now()
	log.Printf("Collecting %s metrics", c.Name())
	if c.PricingMap == nil || time.Now().After(c.NextScrape) {
		log.Println("Refreshing pricing map")
		serviceName, err := c.GetServiceName()
		if err != nil {
			log.Printf("Error getting service name: %s", err)
			return 0
		}
		skus := c.GetPricing(serviceName)
		pricingMap, err := GeneratePricingMap(skus)
		if err != nil {
			log.Printf("Error generating pricing map: %s", err)
			return 0
		}

		c.PricingMap = pricingMap
		c.NextScrape = time.Now().Add(c.config.ScrapeInterval)
		log.Printf("Finished refreshing pricing map in %s", time.Since(start))
	}
	for _, project := range c.Projects {
		instances, err := c.ListInstances(project)
		if err != nil {
			return 0
		}
		for _, instance := range instances {
			cpuCost, ramCost, err := c.PricingMap.GetCostOfInstance(instance)
			if err != nil {
				log.Printf("Could not get cost of instance(%s): %s", instance.Instance, err)
				continue
			}
			InstanceCPUHourlyCost.With(prometheus.Labels{
				"project":      project,
				"instance":     instance.Instance,
				"price_tier":   priceTierForInstance(instance),
				"machine_type": instance.MachineType,
				"region":       instance.Region,
				"family":       instance.Family,
				"provider":     "gcp",
			}).Set(cpuCost)
			InstanceMemoryHourlyCost.With(prometheus.Labels{
				"project":      project,
				"instance":     instance.Instance,
				"price_tier":   priceTierForInstance(instance),
				"machine_type": instance.MachineType,
				"region":       instance.Region,
				"family":       instance.Family,
				"provider":     "gcp",
			}).Set(ramCost)
		}
	}
	log.Printf("Finished collecting GKE metrics in %s", time.Since(start))

	return 1.0
}

func priceTierForInstance(instance *MachineSpec) string {
	if instance.SpotInstance {
		return "spot"
	}
	// TODO: Handle if it's a commitment
	return "ondemand"
}
