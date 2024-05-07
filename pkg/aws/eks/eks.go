package eks

import (
	"context"
	"encoding/json"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/pricing"
	"github.com/aws/aws-sdk-go-v2/service/pricing/types"
	"github.com/prometheus/client_golang/prometheus"

	cloudcostexporter "github.com/grafana/cloudcost-exporter"
	"github.com/grafana/cloudcost-exporter/pkg/provider"
)

const (
	subsystem            = "eks"
	cpuToMemoryCostRatio = .80
)

var (
	InstanceCPUHourlyCostDesc = prometheus.NewDesc(
		prometheus.BuildFQName(cloudcostexporter.MetricPrefix, subsystem, "instance_cpu_usd_per_core_hour"),
		"The cpu cost a compute instance in USD/(core*h)",
		[]string{"instance", "region", "family", "machine_type", "cluster", "price_tier", "provider"},
		nil,
	)
	InstanceMemoryHourlyCostDesc = prometheus.NewDesc(
		prometheus.BuildFQName(cloudcostexporter.MetricPrefix, subsystem, "instance_memory_usd_per_gib_hour"),
		"The memory cost of a compute instance in USD/(GiB*h)",
		[]string{"instance", "region", "family", "machine_type", "cluster", "price_tier", "provider"},
		nil,
	)
)

type Attributes struct {
	Region            string `json:"regionCode"`
	InstanceType      string `json:"instanceType"`
	VCPU              string `json:"vcpu"`
	Memory            string `json:"memory"`
	InstanceFamily    string `json:"instanceFamily"`
	PhysicalProcessor string `json:"physicalProcessor"`
	Tenancy           string `json:"tenancy"`
	MarketOption      string `json:"marketOption"`
	OperatingSystem   string `json:"operatingSystem"`
	ClockSpeed        string `json:"clockSpeed"`
	UsageType         string `json:"usageType"`
}

type productTerm struct {
	Product struct {
		Attributes Attributes
	}
	Terms struct {
		OnDemand map[string]struct {
			PriceDimensions map[string]struct {
				PricePerUnit map[string]string `json:"pricePerUnit"`
			}
		}
	}
}

// StructuredPricingMap collects a map of FamilyPricing structs where the key is the region
type StructuredPricingMap struct {
	// Regions is a map of region code to FamilyPricing
	// key is the region
	// value is a map of instance type to PriceTiers
	Regions map[string]*FamilyPricing
}

// FamilyPricing is a map of instance type to a list of PriceTiers where the key is the ec2 compute instance type
type FamilyPricing struct {
	Family map[string]*PriceTiers // Each Family can have many PriceTiers
}

// ComputePrices holds the price of a compute instances CPU and RAM. The price is in USD
type ComputePrices struct {
	Cpu float64
	Ram float64
}

// PriceTiers holds the on demand and spot prices for a compute instance
type PriceTiers struct {
	OnDemand    *ComputePrices
	Spot        *ComputePrices
	productTerm productTerm
}

func NewStructuredPricingMap() *StructuredPricingMap {
	return &StructuredPricingMap{
		Regions: make(map[string]*FamilyPricing),
	}
}

func (spm *StructuredPricingMap) GeneratePricingMap(prices []string) error {
	for _, product := range prices {
		var productInfo productTerm
		if err := json.Unmarshal([]byte(product), &productInfo); err != nil {
			log.Printf("error decoding product info: %s", err)
			return err
		}
		if productInfo.Product.Attributes.InstanceType == "" {
			// If there are no instance types, let's just continue on. This is the most important key
			continue
		}
		for _, term := range productInfo.Terms.OnDemand {
			for _, priceDimension := range term.PriceDimensions {
				price, err := strconv.ParseFloat(priceDimension.PricePerUnit["USD"], 64)
				if err != nil {
					log.Printf("error parsing price: %s, skipping", err)
				}

				if spm.Regions[productInfo.Product.Attributes.Region] == nil {
					spm.Regions[productInfo.Product.Attributes.Region] = &FamilyPricing{}
					spm.Regions[productInfo.Product.Attributes.Region].Family = make(map[string]*PriceTiers)
				}

				if spm.Regions[productInfo.Product.Attributes.Region].Family[productInfo.Product.Attributes.InstanceType] != nil {
					log.Printf("instance type %s already exists in the map, skipping", productInfo.Product.Attributes.InstanceType)
					continue
				}

				weightedPrice, err := weightedPriceForInstance(price, productInfo)
				if err != nil {
					log.Printf("error calculating weighted price: %s, skipping", err)
					continue
				}
				spm.Regions[productInfo.Product.Attributes.Region].Family[productInfo.Product.Attributes.InstanceType] = &PriceTiers{
					OnDemand:    weightedPrice,
					productTerm: productInfo,
					Spot:        &ComputePrices{}, // TODO: Implement spot pricing
				}
			}
		}
	}
	return nil
}

func weightedPriceForInstance(price float64, product productTerm) (*ComputePrices, error) {
	cpus, err := strconv.ParseFloat(product.Product.Attributes.VCPU, 64)
	if err != nil {
		log.Printf("error parsing cpu count: %s, skipping", err)
		return nil, nil
	}
	if strings.Contains(product.Product.Attributes.Memory, " GiB") {
		product.Product.Attributes.Memory = strings.TrimSuffix(product.Product.Attributes.Memory, " GiB")
	}
	ram, err := strconv.ParseFloat(product.Product.Attributes.Memory, 64)
	if err != nil {
		log.Printf("error parsing ram count: %s, skipping", err)
		return nil, nil
	}
	return &ComputePrices{
		Cpu: price * cpuToMemoryCostRatio / cpus,
		Ram: price * (1 - cpuToMemoryCostRatio) / ram,
	}, nil
}

type Collector struct {
	Region         string
	Profile        string
	Profiles       []string
	ScrapeInterval time.Duration
	pricingMap     *StructuredPricingMap
	pricingService *pricing.Client
	ec2Client      *ec2.Client
}

func (c *Collector) CollectMetrics(metrics chan<- prometheus.Metric) float64 {
	//TODO implement me
	panic("implement me")
}

func (c *Collector) Collect(ch chan<- prometheus.Metric) error {
	//1. Generate a pricing map
	resp, err := c.ec2Client.DescribeRegions(context.Background(), &ec2.DescribeRegionsInput{})
	if err != nil {
		log.Printf("error listing regions: %s", err)
		return err
	}

	if c.pricingMap == nil {
		var prices []string
		for _, region := range resp.Regions {
			priceList, err := c.ListPrices(context.Background(), *region.RegionName)
			if err != nil {
				log.Printf("error listing prices: %s", err)
				return err
			}
			prices = append(prices, priceList...)
		}
		c.pricingMap = NewStructuredPricingMap()
		if err = c.pricingMap.GeneratePricingMap(prices); err != nil {
			log.Printf("error generating pricing map: %s", err)
			return err
		}
	}
	var instances []ec2Types.Instance
	for _, profile := range c.Profiles {
		for _, region := range resp.Regions {
			reservations, err := ListComputeInstances(context.Background(), c.ec2Client, *region.RegionName, profile)
			if err != nil {
				log.Printf("error listing instances: %s", err)
				continue
			}
			for _, reservation := range reservations.Reservations {
				for _, instance := range reservation.Instances {
					instances = append(instances, instance)
				}
			}
		}
	}
	for _, instance := range instances {
		region := *instance.Placement.AvailabilityZone
		region = region[:len(region)-1]
		if _, ok := c.pricingMap.Regions[region]; !ok {
			log.Printf("no pricing map found for region %s", region)
			continue
		}
		price := c.pricingMap.Regions[region].Family[string(instance.InstanceType)]
		if price == nil {
			log.Printf("no price found for instance type %s", instance.InstanceType)
			continue
		}
		clusterName := clusterNameFromInstance(instance)
		if clusterName == "" {
			log.Printf("no cluster name found for instance %s", *instance.PrivateDnsName)
			continue
		}
		if *instance.PrivateDnsName == "" {
			log.Printf("no private dns name found for instance %s", *instance.InstanceId)
			continue
		}

		pricetier := "ondemand"
		if instance.InstanceLifecycle == "spot" {
			pricetier = "spot"
		}

		labelValues := []string{
			*instance.PrivateDnsName,
			region,
			// TODO: Instance Family has a very different connotation in GKE than it does in AWS. Should we align the two?
			price.productTerm.Product.Attributes.InstanceFamily,
			price.productTerm.Product.Attributes.InstanceType,
			clusterName,
			pricetier,
			"eks",
		}
		ch <- prometheus.MustNewConstMetric(InstanceCPUHourlyCostDesc, prometheus.GaugeValue, price.OnDemand.Cpu, labelValues...)
		ch <- prometheus.MustNewConstMetric(InstanceMemoryHourlyCostDesc, prometheus.GaugeValue, price.OnDemand.Ram, labelValues...)
	}
	return nil
}

var clusterTags = []string{"cluster", "eks:cluster-name", "aws:eks:cluster-name"}

func clusterNameFromInstance(instance ec2Types.Instance) string {
	for _, tag := range instance.Tags {
		for _, tagKey := range clusterTags {
			if *tag.Key == tagKey {
				return *tag.Value
			}
		}
	}
	return ""
}

func CollectMetrics(_ chan<- prometheus.Metric) error {
	//TODO implement me
	panic("implement me")
}

func (c *Collector) Describe(ch chan<- *prometheus.Desc) error {
	ch <- InstanceCPUHourlyCostDesc
	ch <- InstanceMemoryHourlyCostDesc
	return nil
}

func (c *Collector) Name() string {
	return "eks"
}

func NewCollector(region string, profile string, scrapeInternal time.Duration, ps *pricing.Client, ec2s *ec2.Client, profiles []string) (*Collector, error) {
	return &Collector{
		Region:         region,
		Profile:        profile,
		Profiles:       profiles,
		ScrapeInterval: scrapeInternal,
		pricingService: ps,
		ec2Client:      ec2s,
	}, nil
}

func (c *Collector) Register(_ provider.Registry) error {
	return nil
}

func (c *Collector) ListPrices(ctx context.Context, region string) ([]string, error) {
	var productOutputs []string
	input := &pricing.GetProductsInput{
		ServiceCode: aws.String("AmazonEC2"),
		Filters: []types.Filter{
			{
				Field: aws.String("regionCode"),
				// TODO: Use the defined enum for this once I figure out how I can import it
				Type:  "TERM_MATCH",
				Value: aws.String(region),
			},
			{
				// Limit output to only base installs
				Field: aws.String("preInstalledSw"),
				Type:  "TERM_MATCH",
				Value: aws.String("NA"),
			},
			{
				// Limit to shared tenancy machines
				Field: aws.String("tenancy"),
				Type:  "TERM_MATCH",
				Value: aws.String("shared"),
			},
			{
				// Limit to compute instances(ie, not bare metal)
				Field: aws.String("productFamily"),
				Type:  "TERM_MATCH",
				Value: aws.String("Compute Instance"),
			},
			{
				// RunInstances is the operation that we're interested in.
				Field: aws.String("operation"),
				Type:  "TERM_MATCH",
				Value: aws.String("RunInstances"),
			},
			{
				// This effectively filters only for ondemand pricing
				Field: aws.String("capacitystatus"),
				Type:  "TERM_MATCH",
				Value: aws.String("Used"),
			},
			{
				// Only care about Linux. If there's a request for windows, remove this flag and expand the pricing map to include a key for operating system
				Field: aws.String("operatingSystem"),
				Type:  "TERM_MATCH",
				Value: aws.String("Linux"),
			},
		},
	}
	products, err := c.pricingService.GetProducts(ctx, input)
	if err != nil {
		return productOutputs, err
	}
	for {
		if products == nil {
			break
		}
		productOutputs = append(productOutputs, products.PriceList...)
		if products.NextToken == nil {
			break
		}
		input.NextToken = products.NextToken
		products, err = c.pricingService.GetProducts(ctx, input)
	}
	return productOutputs, nil
}

func ListComputeInstances(ctx context.Context, c *ec2.Client, region string, profile string) (*ec2.DescribeInstancesOutput, error) {
	options := []func(*awsconfig.LoadOptions) error{awsconfig.WithEC2IMDSRegion()}
	options = append(options, awsconfig.WithRegion(region))
	options = append(options, awsconfig.WithSharedConfigProfile(profile))
	ac, err := awsconfig.LoadDefaultConfig(context.Background(), options...)
	client := ec2.NewFromConfig(ac)
	instances, err := client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{})
	if err != nil {
		return nil, err
	}
	return instances, nil
}

type AMIMap struct {
	// Key is ami group id and value is a list of ec2 instance ids
	Map map[string][]string
}

// https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/billing-info-fields.html
// TODO: Convert this into a const map so that we can lookup the value from list instance output
