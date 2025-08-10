package gke

import (
	"context"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"cloud.google.com/go/billing/apiv1/billingpb"
	"github.com/grafana/cloudcost-exporter/pkg/google/client"

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
	chipset          = `(?P<chipset> (Arm|AMD))` // note the space before core, it _must_ be set
	n1Suffix         = `(?: Predefined)`
	resource         = `(?P<resource>Core|Ram)`
	regionRegex      = `\w+(?: \w+){0,2}`
	computeOptimized = `(?P<optimized> ?Compute optimized)`
	onDemandString   = fmt.Sprintf(`^%v?(?:%v|%v)%v?%v?(?: Instance)? %v running in %v$`,
		spotRegex,
		machineTypeRegex,
		computeOptimized,
		n1Suffix,
		chipset,
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
	compute   map[string]*FamilyPricing
	storage   map[string]*StoragePricing
	gcpClient client.Client
}

// NewPricingMap returns a new PricingMap in a way that can be used afterwards.
func NewPricingMap(ctx context.Context, gcpClient client.Client) (*PricingMap, error) {
	pm := &PricingMap{
		compute:   map[string]*FamilyPricing{},
		storage:   map[string]*StoragePricing{},
		gcpClient: gcpClient,
	}

	if err := pm.Populate(ctx); err != nil {
		return nil, err
	}

	return pm, nil
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

type StoragePrices struct {
	ProvisionedSpaceGiB float64
	Throughput          float64
	IOops               float64
}

// StoragePricing is a map where the key is the storage type and the value is the price
type StoragePricing struct {
	Storage map[string]*StoragePrices
}

func NewStoragePricing() *StoragePricing {
	return &StoragePricing{
		Storage: map[string]*StoragePrices{},
	}
}

func (pm *PricingMap) GetCostOfInstance(instance *client.MachineSpec) (float64, float64, error) {
	if len(pm.compute) == 0 || instance == nil {
		return 0, 0, ErrRegionNotFound
	}
	if _, ok := pm.compute[instance.Region]; !ok {
		return 0, 0, fmt.Errorf("%w: %s", ErrRegionNotFound, instance.Region)
	}
	if _, ok := pm.compute[instance.Region].Family[instance.Family]; !ok {
		return 0, 0, fmt.Errorf("%w: %s", ErrFamilyTypeNotFound, instance.Family)
	}
	priceTiers := pm.compute[instance.Region].Family[instance.Family]
	computePrices := priceTiers.OnDemand
	if instance.SpotInstance {
		computePrices = priceTiers.Spot
	}

	return computePrices.Cpu, computePrices.Ram, nil
}

func (pm *PricingMap) GetCostOfStorage(region, storageClass string) (float64, error) {
	if len(pm.storage) == 0 {
		return 0, ErrRegionNotFound
	}
	if _, ok := pm.storage[region]; !ok {
		return 0, fmt.Errorf("%w: %s", ErrRegionNotFound, region)
	}
	if _, ok := pm.storage[region].Storage[storageClass]; !ok {
		return 0, fmt.Errorf("%w: %s", ErrFamilyTypeNotFound, storageClass)
	}
	return pm.storage[region].Storage[storageClass].ProvisionedSpaceGiB, nil
}

var (
	storageClasses = map[string]string{
		"Storage PD Capacity":         "pd-standard",
		"SSD backed PD Capacity":      "pd-ssd",
		"Balanced PD Capacity":        "pd-balanced",
		"Extreme PD Capacity":         "pd-extreme",
		"Hyperdisk Balanced Capacity": "hyperdisk-balanced",
	}
)

// Populate is responsible for collecting skus related to Compute Engine, parsing out the response, and then populating the pricing map
// with relevant skus.
func (pm *PricingMap) Populate(ctx context.Context) error {
	serviceName, err := pm.gcpClient.GetServiceName(ctx, "Compute Engine")
	if err != nil {
		return fmt.Errorf("%w: %s", ErrInitializingPricingMap, err.Error())
	}
	skus := pm.gcpClient.GetPricing(ctx, serviceName)

	if len(skus) == 0 {
		return ErrSkuNotFound
	}

	return pm.ParseSkus(skus)
}

// ParseSkus accepts a list of skus, parses their content, and updates the pricing map with the appropriate costs.
func (pm *PricingMap) ParseSkus(skus []*billingpb.Sku) error {
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
				if _, ok := pm.compute[data.Region]; !ok {
					pm.compute[data.Region] = NewMachineTypePricing()
				}
				if _, ok := pm.compute[data.Region].Family[data.Description]; !ok {
					pm.compute[data.Region].Family[data.Description] = NewPriceTiers()
				}
				floatPrice := float64(data.Price) * 1e-9
				priceTier := pm.compute[data.Region].Family[data.Description]
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
				if _, ok := pm.storage[data.Region]; !ok {
					pm.storage[data.Region] = NewStoragePricing()
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
				if strings.Contains(data.Description, "Confidential") {
					log.Printf("Storage class contains Confidential: %s\n%s\n", storageClass, data.Description)
					continue
				}
				// First time seen, need to initialize the StoragePrices for the storageClass
				if _, ok := pm.storage[data.Region].Storage[storageClass]; !ok {
					pm.storage[data.Region].Storage[storageClass] = &StoragePrices{}
				}
				if pm.storage[data.Region].Storage[storageClass].ProvisionedSpaceGiB != 0.0 {
					log.Printf("Storage class %s already exists in region %s", storageClass, data.Region)
					continue
				}
				// Switch statement must go here to handle hyperdisk cases, otherwise what's happening is
				// The four dimensions get ignored. There is a sku for:
				// 1. Standard IOPS( Hyperdisk Balanced Storage Pools Standard IOPS - Oregon)
				// 2. Capacity (Hyperdisk Balanced Capacity in Milan)
				// 3. Throughput (Hyperdisk Balanced Throughput in Columbus)
				// 4. High Availability Iops(Hyperdisk Balanced High Availability Iops in Mexico)
				// The current implementation specifically looks for `Hyperdisk Balanced Capacity` to avoid taking the last price that's found
				// Then there is one variation of hyperdisks that are priced differently:
				// 1. Storage Pools Advanced Capacity(Hyperdisk Balanced Storage Pools Advanced Capacity - Mexico)
				pm.storage[data.Region].Storage[storageClass].ProvisionedSpaceGiB = float64(data.Price) * 1e-9 / utils.HoursInMonth
			}
		}
	}
	return nil
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
	if len(pricingInfo.PricingExpression.TieredRates) < 1 {
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
