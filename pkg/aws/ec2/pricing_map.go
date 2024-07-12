package ec2

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/pricing"
	"github.com/aws/aws-sdk-go-v2/service/pricing/types"

	ec2client "github.com/grafana/cloudcost-exporter/pkg/aws/services/ec2"
	pricingClient "github.com/grafana/cloudcost-exporter/pkg/aws/services/pricing"
)

const (
	defaultInstanceFamily = "General purpose"
)

var (
	ErrInstanceTypeAlreadyExists = errors.New("instance type already exists in the map")
	ErrParseAttributes           = errors.New("error parsing attribute")
	ErrRegionNotFound            = errors.New("no region found")
	ErrInstanceTypeNotFound      = errors.New("no instance type found")
	ErrListSpotPrices            = errors.New("error listing spot prices")
	ErrListOnDemandPrices        = errors.New("error listing ondemand prices")
)

// cpuToCostRatio was generated by analysing Grafana Labs spend in GCP and finding the ratio of CPU to Memory spend by instance type.
// It's an imperfect approximation, but it's better than nothing.
var cpuToCostRatio = map[string]float64{
	"Compute optimized": 0.88,
	"Memory optimized":  0.48,
	"General purpose":   0.65,
	"Storage optimized": 0.48,
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
	Family map[string]*Prices // Each Family can have many PriceTiers
}

// ComputePrices holds the price of a ec2 instances CPU and RAM. The price is in USD
type Prices struct {
	Cpu   float64
	Ram   float64
	Total float64
}

func NewStructuredPricingMap() *StructuredPricingMap {
	return &StructuredPricingMap{
		Regions:         make(map[string]*FamilyPricing),
		InstanceDetails: make(map[string]Attributes),
		m:               sync.RWMutex{},
	}
}

// GeneratePricingMap accepts a list of ondemand prices and a list of spot prices.
// The method needs to
// 1. Parse out the ondemand prices and generate a productTerm map for each instance type
// 2. Parse out spot prices and use the productTerm map to generate a spot price map
func (spm *StructuredPricingMap) GeneratePricingMap(ondemandPrices []string, spotPrices []ec2Types.SpotPrice) error {
	for _, product := range ondemandPrices {
		var productInfo productTerm
		if err := json.Unmarshal([]byte(product), &productInfo); err != nil {
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
		spm.Regions[attribute.Region].Family = make(map[string]*Prices)
	}

	if spm.Regions[attribute.Region].Family[attribute.InstanceType] != nil {
		return ErrInstanceTypeAlreadyExists
	}

	weightedPrice, err := weightedPriceForInstance(price, attribute)
	if err != nil {
		return err
	}
	spm.Regions[attribute.Region].Family[attribute.InstanceType] = &Prices{
		Cpu:   weightedPrice.Cpu,
		Ram:   weightedPrice.Ram,
		Total: price,
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

func weightedPriceForInstance(price float64, attributes Attributes) (*Prices, error) {
	cpus, err := strconv.ParseFloat(attributes.VCPU, 64)
	if err != nil {
		return nil, fmt.Errorf("%w %w", ErrParseAttributes, err)
	}
	if strings.Contains(attributes.Memory, " GiB") {
		attributes.Memory = strings.TrimSuffix(attributes.Memory, " GiB")
	}
	ram, err := strconv.ParseFloat(attributes.Memory, 64)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrParseAttributes, err)
	}
	ratio, ok := cpuToCostRatio[attributes.InstanceFamily]
	if !ok {
		log.Printf("no ratio found for instance type %s, defaulting to %s", attributes.InstanceType, defaultInstanceFamily)
		ratio = cpuToCostRatio[defaultInstanceFamily]
	}

	return &Prices{
		Cpu: price * ratio / cpus,
		Ram: price * (1 - ratio) / ram,
	}, nil
}

func (spm *StructuredPricingMap) GetPriceForInstanceType(region string, instanceType string) (*Prices, error) {
	spm.m.RLock()
	defer spm.m.RUnlock()
	if _, ok := spm.Regions[region]; !ok {
		return nil, ErrRegionNotFound
	}
	price := spm.Regions[region].Family[instanceType]
	if price == nil {
		return nil, ErrInstanceTypeNotFound
	}
	return spm.Regions[region].Family[instanceType], nil
}

func (spm *StructuredPricingMap) CheckReadiness() bool {
	// TODO - implement
	return true
}

// Attributes represents ec2 instance attributes that are pulled from AWS api's describing instances.
// It's specifically pulled out of productTerm to enable usage during tests.
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

// productTerm represents the nested json response returned by the AWS pricing API.
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

func ListOnDemandPrices(ctx context.Context, region string, client pricingClient.Pricing) ([]string, error) {
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
				// Limit to ec2 instances(ie, not bare metal)
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

func ListSpotPrices(ctx context.Context, client ec2client.EC2) ([]ec2Types.SpotPrice, error) {
	var spotPrices []ec2Types.SpotPrice
	startTime := time.Now().Add(-time.Hour)
	endTime := time.Now()
	sphi := &ec2.DescribeSpotPriceHistoryInput{
		ProductDescriptions: []string{
			"Linux/UNIX (Amazon VPC)",
		},

		StartTime: &startTime,
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
