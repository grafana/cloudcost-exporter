package compute

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"cloud.google.com/go/billing/apiv1/billingpb"
)

var (
	SkuIsNil           = errors.New("sku is nil")
	SkuNotParsable     = errors.New("can't parse sku")
	SkuNotRelevant     = errors.New("sku isn't relevant for the current use cases")
	PricingDataIsOff   = errors.New("pricing data in sku isn't parsable")
	ErrorInRegions     = errors.New("there is an error in the Regions data")
	RegionNotFound     = errors.New("region wasn't found in pricing map")
	FamilyTypeNotFound = errors.New("family wasn't found in pricing map for this region")
	spotRegex          = `(?P<spot>Spot Preemptible )`
	machineTypeRegex   = `(?P<machineType>\w{1,3})`
	amd                = `(?P<amd> AMD)`
	n1Suffix           = `(?: Predefined)`
	resource           = `(?P<resource>Core|Ram)`
	regionRegex        = `\w+(?: \w+){0,2}`
	computeOptimized   = `(?P<optimized> ?Compute optimized)`
	onDemandString     = fmt.Sprintf(`^%v?(?:%v|%v)%v?%v?(?: Instance)? %v running in %v$`,
		spotRegex,
		machineTypeRegex,
		computeOptimized,
		n1Suffix,
		amd,
		resource,
		regionRegex)
	reOnDemand = regexp.MustCompile(onDemandString)
)

type ComputePrices struct {
	Cpu float64
	Ram float64
}

type PriceTiers struct {
	OnDemand ComputePrices
	Spot     ComputePrices
}

func NewPriceTiers() *PriceTiers {
	return &PriceTiers{
		OnDemand: ComputePrices{
			Cpu: 0,
			Ram: 0,
		},
		Spot: ComputePrices{
			Cpu: 0,
			Ram: 0,
		},
	}
}

type FamilyPricing struct {
	Family map[string]*PriceTiers
}

func NewMachineTypePricing() *FamilyPricing {
	return &FamilyPricing{
		Family: map[string]*PriceTiers{},
	}
}

type StructuredPricingMap struct {
	Regions map[string]*FamilyPricing
}

func NewStructuredPricingMap() *StructuredPricingMap {
	return &StructuredPricingMap{
		Regions: map[string]*FamilyPricing{},
	}
}

func (m StructuredPricingMap) GetCostOfInstance(instance *MachineSpec) (float64, float64, error) {
	if len(m.Regions) == 0 || instance == nil {
		return 0, 0, RegionNotFound
	}
	if _, ok := m.Regions[instance.Region]; !ok {
		return 0, 0, RegionNotFound
	}
	if _, ok := m.Regions[instance.Region].Family[instance.Family]; !ok {
		return 0, 0, FamilyTypeNotFound
	}
	priceTiers := m.Regions[instance.Region].Family[instance.Family]
	computePrices := priceTiers.OnDemand
	if instance.SpotInstance {
		computePrices = priceTiers.Spot
	}

	return computePrices.Cpu, computePrices.Ram, nil
}

func GeneratePricingMap(skus []*billingpb.Sku) (*StructuredPricingMap, error) {
	if len(skus) == 0 {
		return &StructuredPricingMap{}, SkuNotFound
	}
	pricingMap := NewStructuredPricingMap()
	for _, sku := range skus {
		rawData, err := getDataFromSku(sku)

		if errors.Is(err, SkuNotRelevant) {
			continue
		}
		if errors.Is(err, PricingDataIsOff) {
			continue
		}
		if errors.Is(err, SkuNotParsable) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if _, ok := pricingMap.Regions[rawData.Region]; !ok {
			pricingMap.Regions[rawData.Region] = NewMachineTypePricing()
		}
		if _, ok := pricingMap.Regions[rawData.Region].Family[rawData.MachineType]; !ok {
			pricingMap.Regions[rawData.Region].Family[rawData.MachineType] = NewPriceTiers()
		}
		floatPrice := float64(rawData.Price) * 1e-9
		priceTier := pricingMap.Regions[rawData.Region].Family[rawData.MachineType]
		if rawData.PriceTier == Spot {
			if rawData.ComputeResource == Ram {
				priceTier.Spot.Ram = floatPrice
				continue
			}
			priceTier.Spot.Cpu = floatPrice
			continue
		}
		if rawData.ComputeResource == Ram {
			priceTier.OnDemand.Ram = floatPrice
			continue
		}
		priceTier.OnDemand.Cpu = floatPrice
		continue
	}
	return pricingMap, nil
}

type PriceTier int64

const (
	OnDemand PriceTier = iota
	Spot
)

type ComputeResource int64

const (
	Cpu ComputeResource = iota
	Ram
)

type ParsedSkuData struct {
	Region          string
	PriceTier       PriceTier
	Price           int32
	MachineType     string
	ComputeResource ComputeResource
}

func NewParsedSkuData(region string, priceTier PriceTier, price int32, machineType string, computeResource ComputeResource) *ParsedSkuData {
	return &ParsedSkuData{
		Region:          region,
		PriceTier:       priceTier,
		Price:           price,
		MachineType:     machineType,
		ComputeResource: computeResource,
	}
}

var ignoreList = []string{
	"Network",
	"Nvidia",
	"Sole Tenancy",
	"Extreme PD Capacity",
	"Storage PD Capacity",
	"Cloud Interconnect - ",
	"Commitment v1: ",
	"Custom",
	"Micro Instance",
	"Small Instance",
	"Memory-optimized",
}

func getResourceType(resource string) ComputeResource {
	if resource == "Ram" {
		return Ram
	}
	return Cpu
}

func getMatchMap(regex *regexp.Regexp, match []string) map[string]string {
	result := make(map[string]string)
	for i, name := range regex.SubexpNames() {
		if i != 0 && name != "" {
			result[name] = match[i]
		}
	}
	return result
}

func getDataFromSku(sku *billingpb.Sku) (*ParsedSkuData, error) {
	if sku == nil {
		return nil, SkuIsNil
	}
	price, err := getPricingInfoFromSku(sku)
	if err != nil {
		return nil, PricingDataIsOff
	}
	for _, ignoreString := range ignoreList {
		if strings.Contains(sku.Description, ignoreString) {
			return nil, SkuNotRelevant
		}
	}

	if matches := reOnDemand.FindStringSubmatch(sku.Description); len(matches) > 0 {
		matchMap := getMatchMap(reOnDemand, matches)
		machineType := strings.ToLower(matchMap["machineType"])
		if matchMap["optimized"] != "" {
			machineType = "c2"
		}
		priceTier := OnDemand
		if matchMap["spot"] != "" {
			priceTier = Spot
		}
		return NewParsedSkuData(
			sku.ServiceRegions[0],
			priceTier,
			price,
			machineType,
			getResourceType(matchMap["resource"])), nil
	}
	return nil, SkuNotParsable
}
