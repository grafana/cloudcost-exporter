package eks

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/pricing"
	"github.com/aws/aws-sdk-go-v2/service/pricing/types"
	"github.com/prometheus/client_golang/prometheus"

	ec2client "github.com/grafana/cloudcost-exporter/pkg/aws/services/ec2"

	cloudcostexporter "github.com/grafana/cloudcost-exporter"
	"github.com/grafana/cloudcost-exporter/pkg/provider"
)

const (
	subsystem = "eks"
)

var cpuToCostRation = map[string]float64{
	"Compute optimized": 0.88,
	"Memory optimized":  0.48,
	"General purpose":   0.65,
}

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
	m               sync.RWMutex
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
					continue
				}
				err = spm.AddToPricingMap(price, productInfo.Product.Attributes)
				if err != nil {
					log.Printf("error adding to pricing map: %s", err)
					continue
				}
				spm.AddInstanceDetails(productInfo.Product.Attributes)
			}
		}
	}
	for _, spotPrice := range spotPrices {
		region := *spotPrice.AvailabilityZone
		instanceType := string(spotPrice.InstanceType)
		if _, ok := spm.InstanceDetails[instanceType]; !ok {
			log.Printf("no instance details found for instance type %s", instanceType)
			continue
		}
		spotProductTerm := spm.InstanceDetails[instanceType]
		// Override the region with the availability zone
		spotProductTerm.Region = region
		price, err := strconv.ParseFloat(*spotPrice.SpotPrice, 64)
		if err != nil {
			log.Printf("error parsing spot price: %s, skipping", err)
			continue
		}
		err = spm.AddToPricingMap(price, spotProductTerm)
		if err != nil {
			log.Printf("error adding to pricing map: %s", err)
			continue
		}
	}
	return nil
}

// AddToPricingMap adds a price to the pricing map. The price is weighted based upon the instance type's CPU and RAM.
func (spm *StructuredPricingMap) AddToPricingMap(price float64, attribute Attributes) error {
	spm.m.Lock()
	defer spm.m.Unlock()
	if spm.Regions[attribute.Region] == nil {
		spm.Regions[attribute.Region] = &FamilyPricing{}
		spm.Regions[attribute.Region].Family = make(map[string]*ComputePrices)
	}

	if spm.Regions[attribute.Region].Family[attribute.InstanceType] != nil {
		return fmt.Errorf("instance type %s already exists in the map, skipping", attribute.InstanceType)
	}

	weightedPrice, err := weightedPriceForInstance(price, attribute)
	if err != nil {
		return fmt.Errorf("error calculating weighted price: %s, skipping: %w", attribute.InstanceType, err)
	}
	spm.Regions[attribute.Region].Family[attribute.InstanceType] = &ComputePrices{
		Cpu: weightedPrice.Cpu,
		Ram: weightedPrice.Ram,
	}
	return nil
}

func (spm *StructuredPricingMap) AddInstanceDetails(attributes Attributes) {
	spm.m.Lock()
	defer spm.m.Unlock()
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
	ratio := cpuToCostRation[attributes.InstanceFamily]
	return &ComputePrices{
		Cpu: price * ratio / cpus,
		Ram: price * (1 - ratio) / ram,
	}, nil
}

type Collector struct {
	Region         string
	Profile        string
	Profiles       []string
	ScrapeInterval time.Duration
	pricingMap     *StructuredPricingMap
	pricingService *pricing.Client
	ec2Client      ec2client.EC2
	NextScrape     time.Time
}

func (c *Collector) CollectMetrics(metrics chan<- prometheus.Metric) float64 {
	//TODO implement me
	panic("implement me")
}

func (c *Collector) Collect(ch chan<- prometheus.Metric) error {
	resp, err := c.ec2Client.DescribeRegions(context.Background(), &ec2.DescribeRegionsInput{
		// Explicitly set this to false, so we don't get all regions.
		// False is the default, but protects against changes in the API
		AllRegions: aws.Bool(false),
	})

	if err != nil {
		log.Printf("error listing regions: %s", err)
		return err
	}

	if c.pricingMap == nil && time.Now().After(c.NextScrape) {
		var prices []string
		var spotPrices []ec2Types.SpotPrice
		for _, region := range resp.Regions {
			priceList, err := c.ListOnDemandPrices(context.Background(), *region.RegionName, c.pricingService)
			if err != nil {
				log.Printf("error listing prices: %s", err)
				return err
			}
			prices = append(prices, priceList...)
			client, err := newEc2Client(*region.RegionName, c.Profile)
			if err != nil {
				log.Printf("error creating ec2 client: %s", err)
				return err
			}
			spotPriceList, err := ListSpotPrices(context.Background(), client)
			if err != nil {
				log.Printf("error listing spot prices: %s", err)
				return err
			}
			spotPrices = append(spotPrices, spotPriceList...)
			c.NextScrape = time.Now().Add(c.ScrapeInterval)
		}
		c.pricingMap = NewStructuredPricingMap()
		if err = c.pricingMap.GeneratePricingMap(prices, spotPrices); err != nil {
			log.Printf("error generating pricing map: %s", err)
			return err
		}
	}

	for _, profile := range c.Profiles {
		wg := sync.WaitGroup{}
		wg.Add(len(resp.Regions))
		instanceCh := make(chan []ec2Types.Reservation, len(resp.Regions))
		for _, region := range resp.Regions {
			go func(region ec2Types.Region, profile string) {
				defer wg.Done()
				reservations, err := ListComputeInstances(context.Background(), *region.RegionName, profile)
				if err != nil {
					log.Printf("error listing instances: %s", err)
					return
				}
				log.Printf("found %d instances in profile:region %s:%s", len(reservations), profile, *region.RegionName)
				instanceCh <- reservations
			}(region, profile)
		}

		go func() {
			wg.Wait()
			close(instanceCh)
		}()
		c.emitMetricsFromChannel(instanceCh, ch)
	}
	return nil
}

var clusterTags = []string{"cluster", "eks:cluster-name", "aws:eks:cluster-name"}

func (spm *StructuredPricingMap) GetPriceForInstanceType(region string, instanceType string) *ComputePrices {
	spm.m.RLock()
	defer spm.m.RUnlock()
	if _, ok := spm.Regions[region]; !ok {
		log.Printf("no pricing map found for region %s", region)
		return nil
	}
	price := spm.Regions[region].Family[string(instanceType)]
	if price == nil {
		log.Printf("no price found for instance type %s", instanceType)
		return nil
	}
	return spm.Regions[region].Family[instanceType]
}

func (c *Collector) emitMetricsFromChannel(reservationsCh chan []ec2Types.Reservation, ch chan<- prometheus.Metric) {
	for reservations := range reservationsCh {
		for _, reservation := range reservations {
			for _, instance := range reservation.Instances {
				clusterName := clusterNameFromInstance(instance)
				if clusterName == "" {
					log.Printf("no cluster name found for instance %s", *instance.PrivateDnsName)
					continue
				}
				if *instance.PrivateDnsName == "" {
					log.Printf("no private dns name found for instance %s", *instance.InstanceId)
					continue
				}

				region := *instance.Placement.AvailabilityZone

				pricetier := "spot"
				if instance.InstanceLifecycle != "spot" {
					pricetier = "ondemand"
					// Ondemand instances are keyed based upon their region, so we need to remove the availability zone
					region = region[:len(region)-1]
				}
				price := c.pricingMap.GetPriceForInstanceType(region, string(instance.InstanceType))

				labelValues := []string{
					*instance.PrivateDnsName,
					// TODO: Instance Family has a very different connotation in GKE than it does in AWS. Should we align the two?
					c.pricingMap.InstanceDetails[string(instance.InstanceType)].InstanceFamily,
					region,
					string(instance.InstanceType),
					clusterName,
					pricetier,
					"eks",
				}
				ch <- prometheus.MustNewConstMetric(InstanceCPUHourlyCostDesc, prometheus.GaugeValue, price.Cpu, labelValues...)
				ch <- prometheus.MustNewConstMetric(InstanceMemoryHourlyCostDesc, prometheus.GaugeValue, price.Ram, labelValues...)
			}
		}
	}
}

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

func NewCollector(region string, profile string, scrapeInternal time.Duration, ps *pricing.Client, ec2s ec2client.EC2, profiles []string) *Collector {
	return &Collector{
		Region:         region,
		Profile:        profile,
		Profiles:       profiles,
		ScrapeInterval: scrapeInternal,
		pricingService: ps,
		ec2Client:      ec2s,
	}
}

func (c *Collector) Register(_ provider.Registry) error {
	return nil
}

func (c *Collector) ListOnDemandPrices(ctx context.Context, region string, client *pricing.Client) ([]string, error) {
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
		products, err := client.GetProducts(ctx, input)
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

func ListSpotPrices(ctx context.Context, client ec2client.EC2) ([]ec2Types.SpotPrice, error) {
	var spotPrices []ec2Types.SpotPrice
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
			// If there's an error, return the set of processed spotPrices and the error.
			return spotPrices, err
		}
		spotPrices = append(spotPrices, resp.SpotPriceHistory...)
		if resp.NextToken == nil || *resp.NextToken == "" {
			break
		}
		sphi.NextToken = resp.NextToken
	}
	return spotPrices, nil
}

func newEc2Client(region, profile string) (*ec2.Client, error) {
	options := []func(*awsconfig.LoadOptions) error{awsconfig.WithEC2IMDSRegion()}
	options = append(options, awsconfig.WithRegion("us-east1"))
	options = append(options, awsconfig.WithSharedConfigProfile("us-east1"))
	ac, err := awsconfig.LoadDefaultConfig(context.Background(), options...)
	if err != nil {
		return nil, err
	}
	client := ec2.NewFromConfig(ac)
	return client, nil
}
