package compute

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"cloud.google.com/go/billing/apiv1/billingpb"
)

var (
	SkuNotFound        = errors.New("no sku was interested in us")
	SkuIsNil           = errors.New("sku is nil")
	SkuNotParsable     = errors.New("can't parse sku")
	SkuNotRelevant     = errors.New("sku isn't relevant for the current use cases")
	PricingDataIsOff   = errors.New("pricing data in sku isn't parsable")
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

type PriceTier int64

const (
	OnDemand PriceTier = iota
	Spot
)

type Resource int64

const (
	Cpu Resource = iota
	Ram
	Storage
)

const (
	hoursInMonth = 24.35 * 30 // 24.35 is the average amount of hours in a day over a year
)

type ParsedSkuData struct {
	Region          string
	PriceTier       PriceTier
	Price           int32
	Description     string
	ComputeResource Resource
}

func NewParsedSkuData(region string, priceTier PriceTier, price int32, description string, computeResource Resource) *ParsedSkuData {
	return &ParsedSkuData{
		Region:          region,
		PriceTier:       priceTier,
		Price:           price,
		Description:     description,
		ComputeResource: computeResource,
	}
}

type Prices struct {
	Cpu float64
	Ram float64
}

type PriceTiers struct {
	OnDemand Prices
	Spot     Prices
}

func NewPriceTiers() *PriceTiers {
	return &PriceTiers{
		OnDemand: Prices{
			Cpu: 0,
			Ram: 0,
		},
		Spot: Prices{
			Cpu: 0,
			Ram: 0,
		},
	}
}

// StructuredPricingMap is a map of regions to a map of family to price tiers
type StructuredPricingMap struct {
	Compute map[string]*FamilyPricing
	Storage map[string]*StoragePricing
}

// NewStructuredPricingMap returns a new StructuredPricingMap in a way that can be used afterwards.
func NewStructuredPricingMap() *StructuredPricingMap {
	return &StructuredPricingMap{
		Compute: map[string]*FamilyPricing{},
		Storage: map[string]*StoragePricing{},
	}
}

// FamilyPricing is a map where the key is the family and the value is the price tiers
type FamilyPricing struct {
	Family map[string]*PriceTiers
}

func NewMachineTypePricing() *FamilyPricing {
	return &FamilyPricing{
		Family: map[string]*PriceTiers{},
	}
}

// StoragePricing is a map where the key is the storage type and the value is the price
type StoragePricing struct {
	Storage map[string]float64
}

func NewStoragePricing() *StoragePricing {
	return &StoragePricing{
		Storage: map[string]float64{},
	}
}

func (m StructuredPricingMap) GetCostOfInstance(instance *MachineSpec) (float64, float64, error) {
	if len(m.Compute) == 0 || instance == nil {
		return 0, 0, RegionNotFound
	}
	if _, ok := m.Compute[instance.Region]; !ok {
		return 0, 0, fmt.Errorf("%w: %s", RegionNotFound, instance.Region)
	}
	if _, ok := m.Compute[instance.Region].Family[instance.Family]; !ok {
		return 0, 0, fmt.Errorf("%w: %s", FamilyTypeNotFound, instance.Family)
	}
	priceTiers := m.Compute[instance.Region].Family[instance.Family]
	computePrices := priceTiers.OnDemand
	if instance.SpotInstance {
		computePrices = priceTiers.Spot
	}

	return computePrices.Cpu, computePrices.Ram, nil
}

func (m StructuredPricingMap) GetCostOfStorage(region, storageClass string) (float64, error) {
	if len(m.Storage) == 0 {
		return 0, RegionNotFound
	}
	if _, ok := m.Storage[region]; !ok {
		return 0, fmt.Errorf("%w: %s", RegionNotFound, region)
	}
	if _, ok := m.Storage[region].Storage[storageClass]; !ok {
		return 0, fmt.Errorf("%w: %s", FamilyTypeNotFound, storageClass)
	}
	return m.Storage[region].Storage[storageClass], nil
}

var (
	storageClasses = map[string]string{
		"Storage PD Capacity":    "pd-standard",
		"SSD backed PD Capacity": "pd-ssd",
		"Balanced PD Capacity":   "pd-balanced",
		"Extreme PD Capacity":    "pd-extreme",
	}
)

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
		for _, data := range rawData {
			switch data.ComputeResource {
			case Ram, Cpu:
				if _, ok := pricingMap.Compute[data.Region]; !ok {
					pricingMap.Compute[data.Region] = NewMachineTypePricing()
				}
				if _, ok := pricingMap.Compute[data.Region].Family[data.Description]; !ok {
					pricingMap.Compute[data.Region].Family[data.Description] = NewPriceTiers()
				}
				floatPrice := float64(data.Price) * 1e-9
				priceTier := pricingMap.Compute[data.Region].Family[data.Description]
				if data.PriceTier == Spot {
					if data.ComputeResource == Ram {
						priceTier.Spot.Ram = floatPrice
						continue
					}
					priceTier.Spot.Cpu = floatPrice
					continue
				}
				if data.ComputeResource == Ram {
					priceTier.OnDemand.Ram = floatPrice
					continue
				}
				priceTier.OnDemand.Cpu = floatPrice
			case Storage:
				// Right now this is somewhat tightly coupled to GKE persistent volumes.
				// In GKE you can only provision the following classes: https://cloud.google.com/kubernetes-engine/docs/how-to/persistent-volumes/gce-pd-csi-driver#create_a_storageclass
				// For extreme disks, we are ignoring the cost of IOPs, which would be a significant cost(could double cost of disk)
				// TODO(pokom): Add support for other storage classes
				// TODO(pokom): Add support for IOps operations
				if _, ok := pricingMap.Storage[data.Region]; !ok {
					pricingMap.Storage[data.Region] = NewStoragePricing()
				}
				storageClass := ""
				for _, sc := range storageClasses {
					if strings.Contains(data.Description, sc) {
						storageClass = sc
						// Break to prevent overwritting the storage class
						break
					}
				}
				pricingMap.Storage[data.Region].Storage[storageClass] = float64(data.Price) * 1e-9 / hoursInMonth
			}
		}
	}
	return pricingMap, nil
}

var ignoreList = []string{
	"Network",
	"Nvidia",
	"Sole Tenancy",
	"Cloud Interconnect - ",
	"Commitment v1: ",
	"Custom",
	"Micro Instance",
	"Small Instance",
	"Memory-optimized",
}

func getDataFromSku(sku *billingpb.Sku) ([]*ParsedSkuData, error) {

	var parsedSkus []*ParsedSkuData
	if sku == nil {
		return nil, SkuIsNil
	}

	for _, ignoreString := range ignoreList {
		if strings.Contains(sku.Description, ignoreString) {
			return nil, SkuNotRelevant
		}
	}

	if matches := reOnDemand.FindStringSubmatch(sku.Description); len(matches) > 0 {
		price, err := getPricingInfoFromSku(sku)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", PricingDataIsOff, err)
		}
		matchMap := getMatchMap(reOnDemand, matches)
		machineType := strings.ToLower(matchMap["machineType"])
		if matchMap["optimized"] != "" {
			machineType = "c2"
		}
		priceTier := OnDemand
		if matchMap["spot"] != "" {
			priceTier = Spot
		}
		for _, region := range sku.ServiceRegions {
			parsedSku := NewParsedSkuData(
				region,
				priceTier,
				price,
				machineType,
				getResourceType(matchMap["resource"]))
			parsedSkus = append(parsedSkus, parsedSku)
		}
		return parsedSkus, nil
	}
	if sku.Category != nil && sku.Category.ResourceFamily == "Storage" {
		price := sku.PricingInfo[0].PricingExpression.TieredRates[len(sku.PricingInfo[0].PricingExpression.TieredRates)-1].UnitPrice.Nanos
		for _, region := range sku.ServiceRegions {
			parsedSku := NewParsedSkuData(
				region,
				OnDemand,
				price,
				sku.Description,
				Storage)
			parsedSkus = append(parsedSkus, parsedSku)
		}
		return parsedSkus, nil
	}

	return nil, SkuNotParsable
}

// getResourceType will return the resource type for a given resource.
// TODO: Need to ensure GPU's are handled as well, or at the very least we're not mixing GPU's and CPU's up.
func getResourceType(resource string) Resource {
	if resource == "Ram" {
		return Ram
	}
	return Cpu
}

// getPricingInfoFromSku will return the pricing for a given sku.
// Pricing is represented in nanos, so we need to divide by 1e9 to get the price in dollars.
// If there are multiple pricing options, we'll just take the first one.
func getPricingInfoFromSku(sku *billingpb.Sku) (int32, error) {
	if len(sku.PricingInfo) == 0 {
		return 0, fmt.Errorf("no pricing info found for sku %s", sku.Name)
	}
	pricingInfo := sku.PricingInfo[0]
	if pricingInfo.PricingExpression.TieredRates == nil || len(pricingInfo.PricingExpression.TieredRates) < 1 {
		return 0, fmt.Errorf("no tiered rates found for sku %s", sku.Name)
	}
	// TODO: We need to consider if there are many teired rates here. For instance, Storage will have a standard disk that has two rates. The first one is zero for the first GiB, then $/GiB after.
	return pricingInfo.PricingExpression.TieredRates[0].UnitPrice.Nanos, nil
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
