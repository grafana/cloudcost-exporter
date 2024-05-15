package eks

import (
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"

	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

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

func (spm *StructuredPricingMap) GetPriceForInstanceType(region string, instanceType string) (*ComputePrices, error) {
	spm.m.RLock()
	defer spm.m.RUnlock()
	if _, ok := spm.Regions[region]; !ok {
		return nil, ErrRegionNotFound
	}
	price := spm.Regions[region].Family[string(instanceType)]
	if price == nil {
		return nil, ErrInstanceTypeNotFound
	}
	return spm.Regions[region].Family[instanceType], nil
}
