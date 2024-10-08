package compute

import (
	"context"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	billingv1 "cloud.google.com/go/billing/apiv1"
	"cloud.google.com/go/billing/apiv1/billingpb"

	"github.com/grafana/cloudcost-exporter/pkg/google/billing"
	"github.com/grafana/cloudcost-exporter/pkg/utils"
)

var (
	ErrSkuNotFound            = errors.New("no sku was interested in us")
	ErrSkuIsNil               = errors.New("sku is nil")
	ErrSkuNotParsable         = errors.New("can't parse sku")
	ErrSkuNotRelevant         = errors.New("sku isn't relevant for the current use cases")
	ErrPricingDataIsOff       = errors.New("pricing data in sku isn't parsable")
	ErrRegionNotFound         = errors.New("region wasn't found in pricing map")
	ErrFamilyTypeNotFound     = errors.New("family wasn't found in pricing map for this region")
	ErrInitializingPricingMap = errors.New("failed to populate pricing map")

	spotRegex        = `(?P<spot>Spot Preemptible )`
	machineTypeRegex = `(?P<machineType>\w{1,3})`
	amd              = `(?P<amd> AMD)`
	n1Suffix         = `(?: Predefined)`
	resource         = `(?P<resource>Core|Ram)`
	regionRegex      = `\w+(?: \w+){0,2}`
	computeOptimized = `(?P<optimized> ?Compute optimized)`
	onDemandString   = fmt.Sprintf(`^%v?(?:%v|%v)%v?%v?(?: Instance)? %v running in %v$`,
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

const PriceRefreshInterval = 24 * time.Hour

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

// PricingMap is a map of regions to a map of family to price tiers
type PricingMap struct {
	Compute map[string]*FamilyPricing
	Storage map[string]*StoragePricing
}

// NewPricingMap returns a new PricingMap in a way that can be used afterwards.
func NewPricingMap(ctx context.Context, billingService *billingv1.CloudCatalogClient) (*PricingMap, error) {
	pm := &PricingMap{
		Compute: map[string]*FamilyPricing{},
		Storage: map[string]*StoragePricing{},
	}

	err := pm.Populate(ctx, billingService)

	if err != nil {
		return nil, err
	}

	return pm, nil
}

// NewComputePricingMap returns a new PricingMap in a way that can be used afterwards.
func NewComputePricingMap() *PricingMap {
	return &PricingMap{
		Compute: map[string]*FamilyPricing{},
		Storage: map[string]*StoragePricing{},
	}
}

func (pm *PricingMap) CheckReadiness() bool {
	// TODO - implement locking on the pricing map
	return true
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

func (m PricingMap) GetCostOfInstance(instance *MachineSpec) (float64, float64, error) {
	if len(m.Compute) == 0 || instance == nil {
		return 0, 0, ErrRegionNotFound
	}
	if _, ok := m.Compute[instance.Region]; !ok {
		return 0, 0, fmt.Errorf("%w: %s", ErrRegionNotFound, instance.Region)
	}
	if _, ok := m.Compute[instance.Region].Family[instance.Family]; !ok {
		return 0, 0, fmt.Errorf("%w: %s", ErrFamilyTypeNotFound, instance.Family)
	}
	priceTiers := m.Compute[instance.Region].Family[instance.Family]
	computePrices := priceTiers.OnDemand
	if instance.SpotInstance {
		computePrices = priceTiers.Spot
	}

	return computePrices.Cpu, computePrices.Ram, nil
}

func (m PricingMap) GetCostOfStorage(region, storageClass string) (float64, error) {
	if len(m.Storage) == 0 {
		return 0, ErrRegionNotFound
	}
	if _, ok := m.Storage[region]; !ok {
		return 0, fmt.Errorf("%w: %s", ErrRegionNotFound, region)
	}
	if _, ok := m.Storage[region].Storage[storageClass]; !ok {
		return 0, fmt.Errorf("%w: %s", ErrFamilyTypeNotFound, storageClass)
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

func (pm *PricingMap) Populate(ctx context.Context, billingService *billingv1.CloudCatalogClient) error {
	serviceName, err := billing.GetServiceName(ctx, billingService, "Compute Engine")
	if err != nil {
		return fmt.Errorf("%w: %s", ErrInitializingPricingMap, err.Error())
	}
	skus := billing.GetPricing(ctx, billingService, serviceName)

	if len(skus) == 0 {
		return ErrSkuNotFound
	}

	for _, sku := range skus {
		rawData, err := getDataFromSku(sku)

		if errors.Is(err, ErrSkuNotRelevant) {
			continue
		}
		if errors.Is(err, ErrPricingDataIsOff) {
			continue
		}
		if errors.Is(err, ErrSkuNotParsable) {
			continue
		}
		if err != nil {
			return err
		}
		for _, data := range rawData {
			switch data.ComputeResource {
			case Ram, Cpu:
				if _, ok := pm.Compute[data.Region]; !ok {
					pm.Compute[data.Region] = NewMachineTypePricing()
				}
				if _, ok := pm.Compute[data.Region].Family[data.Description]; !ok {
					pm.Compute[data.Region].Family[data.Description] = NewPriceTiers()
				}
				floatPrice := float64(data.Price) * 1e-9
				priceTier := pm.Compute[data.Region].Family[data.Description]
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
				if _, ok := pm.Storage[data.Region]; !ok {
					pm.Storage[data.Region] = NewStoragePricing()
				}
				storageClass := ""
				for description, sc := range storageClasses {
					// We check to see if the description starts with the storage class name
					// This is primarily because this could return a false positive in cases of Regional storage which
					// has a similar description.
					if strings.Index(data.Description, description) == 0 {
						storageClass = sc
						// Break to prevent overwritting the storage class
						break
					}
				}
				if storageClass == "" {
					log.Printf("Storage class not found for %s. Skipping", data.Description)
					continue
				}
				if pm.Storage[data.Region].Storage[storageClass] != 0 {
					log.Printf("Storage class %s already exists in region %s", storageClass, data.Region)
					continue
				}
				pm.Storage[data.Region].Storage[storageClass] = float64(data.Price) * 1e-9 / utils.HoursInMonth
			}
		}
	}
	return nil
}

// Paula: deprecate this function in favour of func (pm *PricingMap) Populate(skus []*billingpb.Sku) (*PricingMap, error)
func GeneratePricingMap(skus []*billingpb.Sku) (*PricingMap, error) {
	if len(skus) == 0 {
		return &PricingMap{}, ErrSkuNotFound
	}
	pricingMap := NewComputePricingMap()
	for _, sku := range skus {
		rawData, err := getDataFromSku(sku)

		if errors.Is(err, ErrSkuNotRelevant) {
			continue
		}
		if errors.Is(err, ErrPricingDataIsOff) {
			continue
		}
		if errors.Is(err, ErrSkuNotParsable) {
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
				for description, sc := range storageClasses {
					// We check to see if the description starts with the storage class name
					// This is primarily because this could return a false positive in cases of Regional storage which
					// has a similar description.
					if strings.Index(data.Description, description) == 0 {
						storageClass = sc
						// Break to prevent overwritting the storage class
						break
					}
				}
				if storageClass == "" {
					log.Printf("Storage class not found for %s. Skipping", data.Description)
					continue
				}
				if pricingMap.Storage[data.Region].Storage[storageClass] != 0 {
					log.Printf("Storage class %s already exists in region %s", storageClass, data.Region)
					continue
				}
				pricingMap.Storage[data.Region].Storage[storageClass] = float64(data.Price) * 1e-9 / utils.HoursInMonth
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
		return nil, ErrSkuIsNil
	}

	for _, ignoreString := range ignoreList {
		if strings.Contains(sku.Description, ignoreString) {
			return nil, ErrSkuNotRelevant
		}
	}

	if matches := reOnDemand.FindStringSubmatch(sku.Description); len(matches) > 0 {
		price, err := getPricingInfoFromSku(sku)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrPricingDataIsOff, err)
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

	return nil, ErrSkuNotParsable
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
