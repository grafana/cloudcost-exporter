package cloudsql

import (
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"cloud.google.com/go/billing/apiv1/billingpb"
	"github.com/grafana/cloudcost-exporter/pkg/google/client"
	sqladmin "google.golang.org/api/sqladmin/v1beta4"
)

// instanceSpec represents the compute specifications of a database instance
// if RAM is 0 it means it should be matched from standard pricing data for the tier instead of a custom one
type instanceSpec struct {
	cpu      int
	ram      int
	tier     string
	tierType string
	isCustom bool
}

type priceMatch struct {
	pricePerHour float64
	skuID        string
	description  string
	isCustom     bool
	cpuPrice     float64 // per vCPU per hour if custom
	ramPrice     float64 // per GB per hour if custom
}

type pricingMap struct {
	logger    *slog.Logger
	gcpClient client.Client
	mu        sync.RWMutex
	skus      []*billingpb.Sku
}

type instanceTraits struct {
	region       string
	dbType       string
	availability string
	spec         *instanceSpec
}

var (
	ErrInstanceNotFound      = errors.New("instance not found")
	ErrPriceNotFound         = errors.New("price not found for instance")
	ErrInvalidTier           = errors.New("invalid tier format")
	ErrRegionNotFound        = errors.New("region not found in pricing")
	ErrComponentPriceMissing = errors.New("component pricing (CPU/RAM) not available")

	serviceName = "Cloud SQL"

	// Tier parsing regex
	tierCustomRegex = regexp.MustCompile(`^db-custom-(\d+)-(\d+)$`)
	// Micro/small tier regex (e.g., db-f1-micro, db-g1-small). It has a different format than the generic regex.
	tierMicroSmallRegex = regexp.MustCompile(`^db-([a-z0-9]+)-([a-z]+)$`)
	// Generic regex that matches various db types and tier categories (e.g., standard, highmem, custom, etc.)
	tierGenericRegex = regexp.MustCompile(`^db-([a-z0-9]+)-([a-z0-9]+)-(?:([a-z0-9]+)-)?(\d+)(?:-(\d+))?$`)
)

// Price calculation logic:
// 1. If the instance is a custom instance, use the custom pricing logic. This depends on vCPU and RAM.
//  - Calculate the total price by multiplying the price of the CPU by the number of vCPUs and the price of the RAM by the amount of RAM.
// 2. If the instance is a standard instance, use the standard pricing logic:
//  - Find the SKU that is relevant to the standard instance. The sku is not part of the instance payload, so we need to find it
//  by matching the region, database type, and availability type.
//  - Micro/small tiers are matched by the tier name in the description, since they have no vCPU or RAM.
//  - Find the price for the SKU.
//  - Return the price.

func newPricingMap(logger *slog.Logger, gcpClient client.Client) *pricingMap {
	return &pricingMap{
		logger:    logger,
		gcpClient: gcpClient,
	}
}

func priceForSKU(sku *billingpb.Sku) (float64, bool) {
	if len(sku.PricingInfo) == 0 {
		return 0, false
	}
	if len(sku.PricingInfo[0].PricingExpression.TieredRates) == 0 {
		return 0, false
	}

	price := float64(sku.PricingInfo[0].PricingExpression.TieredRates[0].UnitPrice.Nanos) / 1e9
	if sku.PricingInfo[0].PricingExpression.TieredRates[0].UnitPrice.Units > 0 {
		price = float64(sku.PricingInfo[0].PricingExpression.TieredRates[0].UnitPrice.Units) + price
	}
	return price, true
}

func getInstanceSpecFromTier(tier string) (*instanceSpec, error) {
	// Custom tier: db-custom-{vCPU}-{RAM_MB}
	if matches := tierCustomRegex.FindStringSubmatch(tier); matches != nil {
		return &instanceSpec{
			cpu:      parseInt(matches[1]),
			ram:      parseInt(matches[2]),
			tier:     tier,
			isCustom: true,
		}, nil
	}

	if matches := tierMicroSmallRegex.FindStringSubmatch(tier); matches != nil {
		return &instanceSpec{
			cpu:      0,
			ram:      0,
			tier:     tier,
			tierType: matches[2],
			isCustom: false,
		}, nil
	}

	// Generic tiers: db-{machine_family}-{tier}-{vCPU}-{RAM_MB}
	if matches := tierGenericRegex.FindStringSubmatch(tier); matches != nil {
		cpuStr := matches[4]
		if cpuStr == "" {
			return nil, fmt.Errorf("%w: could not extract vCPU from tier %s", ErrInvalidTier, tier)
		}
		cpu := parseInt(cpuStr)

		tierType := matches[2]

		return &instanceSpec{
			cpu:      cpu,
			ram:      0,
			tier:     tier,
			tierType: tierType,
			isCustom: false,
		}, nil
	}

	return nil, fmt.Errorf("%w: %s", ErrInvalidTier, tier)
}

func parseInt(s string) int {
	var n int
	fmt.Sscanf(s, "%d", &n)
	return n
}

// extractSpecsFromDescription extracts vCPU and RAM from price description
func extractSpecsFromDescription(description string) (cpu int, ramMB int, ok bool) {
	descLower := strings.ToLower(description)

	cpuMatch := regexp.MustCompile(`(\d+)\s*vcpu`).FindStringSubmatch(descLower)
	if cpuMatch == nil {
		return 0, 0, false
	}
	cpu = parseInt(cpuMatch[1])

	ramMatch := regexp.MustCompile(`(\d+\.?\d*)\s*(gb|mb)\s*ram`).FindStringSubmatch(descLower)
	if ramMatch == nil {
		return 0, 0, false
	}
	ramFloat, err := strconv.ParseFloat(ramMatch[1], 64)
	if err != nil {
		return 0, 0, false
	}
	ramUnit := strings.ToLower(ramMatch[2])
	if ramUnit == "gb" {
		ramMB = int(ramFloat * 1024)
	} else {
		ramMB = int(ramFloat)
	}

	return cpu, ramMB, true
}

func getDatabaseType(version string) string {
	versionUpper := strings.ToUpper(version)
	if strings.Contains(versionUpper, "MYSQL") {
		return "MYSQL"
	}
	if strings.Contains(versionUpper, "POSTGRES") {
		return "POSTGRES"
	}
	return ""
}

func getAvailabilityType(availType string) string {
	availUpper := strings.ToUpper(availType)
	if availUpper == "ZONAL" {
		return "ZONAL"
	}
	if availUpper == "REGIONAL" {
		return "REGIONAL"
	}
	return availUpper
}

func (pm *pricingMap) matchInstancePrice(instance *sqladmin.DatabaseInstance) (*priceMatch, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	it, err := pm.extractInstanceInfo(instance)
	if err != nil {
		return nil, err
	}

	// For standard instances, search through SKUs to find matching price
	if !it.spec.isCustom {
		priceMatch, err := pm.findStandardInstancePrice(it)
		if err == nil {
			return priceMatch, nil
		}
	}

	// For custom configurations, use custom pricing
	return pm.calculateCustomPrice(it.region, it.spec)
}

func (pm *pricingMap) extractInstanceInfo(instance *sqladmin.DatabaseInstance) (instanceTraits, error) {
	if instance.Settings == nil || instance.Settings.Tier == "" {
		pm.logger.Error("instance missing tier information")
		return instanceTraits{}, fmt.Errorf("instance missing tier information")
	}

	spec, err := getInstanceSpecFromTier(instance.Settings.Tier)
	if err != nil {
		pm.logger.Error("error parsing tier", "error", err)
		return instanceTraits{}, err
	}

	dbType := getDatabaseType(instance.DatabaseVersion)
	if dbType == "" {
		pm.logger.Error("unknown database type", "version", instance.DatabaseVersion)
		return instanceTraits{}, fmt.Errorf("unknown database type: %s", instance.DatabaseVersion)
	}

	availability := getAvailabilityType(instance.Settings.AvailabilityType)
	if availability == "" {
		pm.logger.Error("unknown availability type", "availability", instance.Settings.AvailabilityType)
		return instanceTraits{}, fmt.Errorf("unknown availability type: %s", instance.Settings.AvailabilityType)
	}

	region := instance.Region
	if region == "" {
		pm.logger.Error("instance missing region")
		return instanceTraits{}, fmt.Errorf("instance missing region")
	}

	return instanceTraits{
		region:       region,
		dbType:       dbType,
		availability: availability,
		spec:         spec,
	}, nil
}

func isCustomPricingSku(sku *billingpb.Sku) bool {
	if len(sku.PricingInfo) == 0 {
		return false
	}
	usageUnit := sku.PricingInfo[0].PricingExpression.UsageUnit
	descLower := strings.ToLower(sku.Description)

	// Custom pricing SKUs have usage units like "h" (CPU) or "GiBy" (RAM)
	// and descriptions that mention CPU or RAM as components
	return (usageUnit == "h" && strings.Contains(descLower, "cpu")) ||
		(strings.Contains(usageUnit, "GiBy") && strings.Contains(descLower, "ram"))
}

func (pm *pricingMap) findStandardInstancePrice(it instanceTraits) (*priceMatch, error) {
	for _, sku := range pm.skus {
		if sku.Category == nil || sku.Category.ServiceDisplayName != serviceName {
			continue
		}

		// Skip component pricing SKUs (these are for custom instances)
		if isCustomPricingSku(sku) {
			continue
		}

		// Check region
		if len(sku.GeoTaxonomy.Regions) == 0 || !isSkuInRegion(sku, it.region) {
			continue
		}

		// Check database type
		skuDbType := dbTypeFromDescription(sku.Description)
		if skuDbType != it.dbType {
			continue
		}

		// Check availability
		skuAvailability := availabilityFromDescription(sku.Description)
		if skuAvailability != it.availability {
			continue
		}

		// For micro/small tiers, match by tier name in description
		if it.spec.cpu == 0 {
			descLower := strings.ToLower(sku.Description)
			tierLower := strings.ToLower(it.spec.tier)
			if !strings.Contains(descLower, tierLower) {
				continue
			}
		} else {
			// Extract and match vCPU for standard tiers
			vcpu, _, ok := extractSpecsFromDescription(sku.Description)
			if !ok || vcpu != it.spec.cpu {
				continue
			}
		}

		// Extract price
		price, ok := priceForSKU(sku)
		if !ok {
			continue
		}

		return &priceMatch{
			pricePerHour: price,
			skuID:        sku.SkuId,
			description:  sku.Description,
			isCustom:     false,
			cpuPrice:     0,
			ramPrice:     0,
		}, nil
	}

	return nil, ErrPriceNotFound
}

// calculateCustomPrice calculates price from CPU and RAM components for a certain region
func (pm *pricingMap) calculateCustomPrice(region string, spec *instanceSpec) (*priceMatch, error) {
	var cpuPrice, ramPrice float64
	var cpuSkuID, ramSkuID string

	for _, sku := range pm.skus {
		if sku.Category == nil || sku.Category.ServiceDisplayName != serviceName {
			continue
		}

		if !isCustomPricingSku(sku) {
			continue
		}

		if len(sku.GeoTaxonomy.Regions) == 0 || !isSkuInRegion(sku, region) {
			continue
		}

		price, ok := priceForSKU(sku)
		if !ok {
			continue
		}

		usageUnit := sku.PricingInfo[0].PricingExpression.UsageUnit
		descLower := strings.ToLower(sku.Description)

		// Extract CPU price (per vCPU per hour)
		// CPU component SKUs have usage unit "h" and mention "cpu" in description
		if usageUnit == "h" && strings.Contains(descLower, "cpu") {
			cpuPrice = price
			cpuSkuID = sku.SkuId
		}

		// Extract RAM price (per GB per hour)
		// RAM component SKUs have usage unit containing "GiBy" and mention "ram" in description
		if strings.Contains(usageUnit, "GiBy") && strings.Contains(descLower, "ram") {
			ramPrice = price
			ramSkuID = sku.SkuId
		}
	}

	if cpuPrice == 0 || ramPrice == 0 {
		return nil, fmt.Errorf("%w: region=%s (cpuPrice=%f, ramPrice=%f)", ErrComponentPriceMissing, region, cpuPrice, ramPrice)
	}

	ramGB := float64(spec.ram) / 1024.0
	totalPrice := float64(spec.cpu)*cpuPrice + ramGB*ramPrice

	skuID := fmt.Sprintf("component-based:%s+%s", cpuSkuID, ramSkuID)

	return &priceMatch{
		pricePerHour: totalPrice,
		skuID:        skuID,
		description:  fmt.Sprintf("Component pricing: %d vCPU + %.2fGB RAM", spec.cpu, ramGB),
		isCustom:     true,
		cpuPrice:     cpuPrice,
		ramPrice:     ramPrice,
	}, nil
}

func isSkuInRegion(sku *billingpb.Sku, region string) bool {
	for _, skuRegion := range sku.GeoTaxonomy.Regions {
		if strings.EqualFold(skuRegion, region) {
			return true
		}
	}
	return false
}

func dbTypeFromDescription(description string) string {
	descriptionUpper := strings.ToUpper(description)
	if strings.Contains(descriptionUpper, "MYSQL") {
		return "MYSQL"
	}
	if strings.Contains(descriptionUpper, "POSTGRES") {
		return "POSTGRES"
	}
	return ""
}

func availabilityFromDescription(description string) string {
	descriptionUpper := strings.ToUpper(description)
	if strings.Contains(descriptionUpper, "ZONAL") {
		return "ZONAL"
	}
	if strings.Contains(descriptionUpper, "REGIONAL") {
		return "REGIONAL"
	}
	return ""
}
