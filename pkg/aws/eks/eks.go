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
	Regions         map[string]*FamilyPricing
	InstanceDetails map[string]Attributes
}

// FamilyPricing is a map of instance type to a list of PriceTiers where the key is the ec2 compute instance type
type FamilyPricing struct {
	Family map[string]*ComputePrices // Each Family can have many PriceTiers
}

// ComputePrices holds the price of a compute instances CPU and RAM. The price is in USD
type ComputePrices struct {
	Cpu float64
	Ram float64
}

func NewStructuredPricingMap() *StructuredPricingMap {
	return &StructuredPricingMap{
		Regions:         make(map[string]*FamilyPricing),
		InstanceDetails: make(map[string]Attributes),
	}
}

// GeneratePricingMap accepts a list of ondemand prices and a list of spot prices.
// The method needs to
// 1. Parse out the ondemand prices and generate a productTerm map for each instance type
// 2. Parse out spot prices and use the productTerm map to generate a spot price map
func (spm *StructuredPricingMap) GeneratePricingMap(prices []string, spotPrices []ec2Types.SpotPrice) error {
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
					spm.Regions[productInfo.Product.Attributes.Region].Family = make(map[string]*ComputePrices)
				}

				if spm.Regions[productInfo.Product.Attributes.Region].Family[productInfo.Product.Attributes.InstanceType] != nil {
					log.Printf("instance type %s already exists in the map, skipping", productInfo.Product.Attributes.InstanceType)
					continue
				}

				weightedPrice, err := weightedPriceForInstance(price, productInfo.Product.Attributes)
				if err != nil {
					log.Printf("error calculating weighted price: %s, skipping", err)
					continue
				}
				spm.Regions[productInfo.Product.Attributes.Region].Family[productInfo.Product.Attributes.InstanceType] = &ComputePrices{
					Cpu: weightedPrice.Cpu,
					Ram: weightedPrice.Ram,
				}
				spm.AddInstanceDetails(productInfo.Product.Attributes)
			}
		}
	}
	for _, spotPrice := range spotPrices {
		region := *spotPrice.AvailabilityZone
		instanceType := string(spotPrice.InstanceType)
		if _, ok := spm.Regions[region]; !ok {
			spm.Regions[region] = &FamilyPricing{}
			spm.Regions[region].Family = make(map[string]*ComputePrices)
		}

		if _, ok := spm.InstanceDetails[instanceType]; !ok {
			log.Printf("no instance details found for instance type %s", instanceType)
			continue
		}
		spotProductTerm := spm.InstanceDetails[instanceType]
		price, err := strconv.ParseFloat(*spotPrice.SpotPrice, 64)
		if err != nil {
			log.Printf("error parsing price: %s, skipping", err)
			continue
		}

		weightedPrice, err := weightedPriceForInstance(price, spotProductTerm)
		if err != nil {
			log.Printf("error calculating weighted price: %s, skipping", err)
			continue
		}
		spm.Regions[region].Family[instanceType] = &ComputePrices{
			Cpu: weightedPrice.Cpu,
			Ram: weightedPrice.Ram,
		}
	}
	return nil
}

func (spm *StructuredPricingMap) AddInstanceDetails(attributes Attributes) {
	if _, ok := spm.InstanceDetails[attributes.InstanceType]; !ok {
		spm.InstanceDetails[attributes.InstanceType] = attributes
	}
}

func weightedPriceForInstance(price float64, attributes Attributes) (*ComputePrices, error) {
	cpus, err := strconv.ParseFloat(attributes.VCPU, 64)
	if err != nil {
		log.Printf("error parsing cpu count: %s, skipping", err)
		return nil, nil
	}
	if strings.Contains(attributes.Memory, " GiB") {
		attributes.Memory = strings.TrimSuffix(attributes.Memory, " GiB")
	}
	ram, err := strconv.ParseFloat(attributes.Memory, 64)
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
		var spotPrices []ec2Types.SpotPrice
		for _, region := range resp.Regions {
			priceList, err := c.ListOnDemandPrices(context.Background(), *region.RegionName)
			if err != nil {
				log.Printf("error listing prices: %s", err)
				return err
			}
			prices = append(prices, priceList...)
			spotPriceList, err := ListSpotPrices(context.Background(), *region.RegionName, c.Profile)
			if err != nil {
				log.Printf("error listing spot prices: %s", err)
				return err
			}
			spotPrices = append(spotPrices, spotPriceList...)
		}
		c.pricingMap = NewStructuredPricingMap()
		if err = c.pricingMap.GeneratePricingMap(prices, spotPrices); err != nil {
			log.Printf("error generating pricing map: %s", err)
			return err
		}
	}
	var instances []ec2Types.Instance
	for _, profile := range c.Profiles {
		for _, region := range resp.Regions {
			reservations, err := ListComputeInstances(context.Background(), *region.RegionName, profile)
			if err != nil {
				log.Printf("error listing instances: %s", err)
				continue
			}
			for _, reservation := range reservations {
				instances = append(instances, reservation.Instances...)
			}
		}
	}
	for _, instance := range instances {
		region := *instance.Placement.AvailabilityZone
		// Remove the last character from the region to get the region code
		region = region[:len(region)-1]
		pricetier := "ondemand"
		if instance.InstanceLifecycle == "spot" {
			pricetier = "spot"
			// Spot instances price map is keyed be availability zone
			region = *instance.Placement.AvailabilityZone
		}
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

		labelValues := []string{
			*instance.PrivateDnsName,
			region,
			// TODO: Instance Family has a very different connotation in GKE than it does in AWS. Should we align the two?
			"",
			string(instance.InstanceType),
			clusterName,
			pricetier,
			"eks",
		}
		ch <- prometheus.MustNewConstMetric(InstanceCPUHourlyCostDesc, prometheus.GaugeValue, price.Cpu, labelValues...)
		ch <- prometheus.MustNewConstMetric(InstanceMemoryHourlyCostDesc, prometheus.GaugeValue, price.Ram, labelValues...)
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

func (c *Collector) ListOnDemandPrices(ctx context.Context, region string) ([]string, error) {
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
				Value: aws.String("UnusedCapacityReservation"),
			},
			{
				// Only care about Linux. If there's a request for windows, remove this flag and expand the pricing map to include a key for operating system
				Field: aws.String("operatingSystem"),
				Type:  "TERM_MATCH",
				Value: aws.String("Linux"),
			},
		},
	}

	for {
		products, err := c.pricingService.GetProducts(ctx, input)
		if err != nil {
			return productOutputs, err
		}

		if products == nil {
			break
		}

		productOutputs = append(productOutputs, products.PriceList...)
		if products.NextToken == nil {
			break
		}
		input.NextToken = products.NextToken
	}
	return productOutputs, nil
}

func ListComputeInstances(ctx context.Context, region string, profile string) ([]ec2Types.Reservation, error) {
	options := []func(*awsconfig.LoadOptions) error{awsconfig.WithEC2IMDSRegion()}
	options = append(options, awsconfig.WithRegion(region))
	options = append(options, awsconfig.WithSharedConfigProfile(profile))
	ac, err := awsconfig.LoadDefaultConfig(context.Background(), options...)
	if err != nil {
		return nil, err
	}
	client := ec2.NewFromConfig(ac)
	dii := &ec2.DescribeInstancesInput{
		// TODO: Is 1000 appropriate?
		MaxResults: aws.Int32(1000),
	}
	var instances []ec2Types.Reservation
	for {
		resp, err := client.DescribeInstances(ctx, dii)
		if err != nil {
			return nil, err
		}
		instances = append(instances, resp.Reservations...)
		if resp.NextToken == nil || *resp.NextToken == "" {
			break
		}
		dii.NextToken = resp.NextToken
	}

	return instances, nil
}

func ListSpotPrices(ctx context.Context, region string, profile string) ([]ec2Types.SpotPrice, error) {
	options := []func(*awsconfig.LoadOptions) error{awsconfig.WithEC2IMDSRegion()}
	options = append(options, awsconfig.WithRegion(region))
	options = append(options, awsconfig.WithSharedConfigProfile(profile))
	ac, err := awsconfig.LoadDefaultConfig(context.Background(), options...)
	if err != nil {
		return nil, err
	}
	client := ec2.NewFromConfig(ac)
	spotPrices := []ec2Types.SpotPrice{}
	// TODO: What's the most accurate way to get just the last spot price? We're not trying to get a history
	starTime := time.Now().Add(-time.Hour)
	endTime := time.Now()
	sphi := &ec2.DescribeSpotPriceHistoryInput{
		ProductDescriptions: []string{
			"Linux/UNIX (Amazon VPC)",
		},

		StartTime: &starTime,
		EndTime:   &endTime,
	}
	for {
		resp, err := client.DescribeSpotPriceHistory(ctx, sphi)
		if err != nil {
			break
		}
		spotPrices = append(spotPrices, resp.SpotPriceHistory...)
		if resp.NextToken == nil || *resp.NextToken == "" {
			break
		}
		sphi.NextToken = resp.NextToken
	}
	return spotPrices, nil
}

type AMIMap struct {
	// Key is ami group id and value is a list of ec2 instance ids
	Map map[string][]string
}

// https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/billing-info-fields.html
// TODO: Convert this into a const map so that we can lookup the value from list instance output
