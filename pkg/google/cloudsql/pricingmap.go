package cloudsql

import (
	"context"
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
	ErrInstanceNotFound   = errors.New("instance not found")
	ErrPriceNotFound      = errors.New("price not found for instance")
	ErrInvalidTier        = errors.New("invalid tier format")
	ErrRegionNotFound     = errors.New("region not found in pricing")
	ErrCustomPriceMissing = errors.New("custom pricing (CPU/RAM) not available")

	serviceName = "Cloud SQL"

	// Tier parsing regex
	tierCustomRegex = regexp.MustCompile(`^db-custom-(\d+)-(\d+)$`)
	// Micro/small tier regex (e.g., db-f1-micro, db-g1-small). It has a different format than the generic regex.
	tierMicroSmallRegex = regexp.MustCompile(`^db-([a-z0-9]+)-([a-z]+)$`)
	// Special tier regex for tiers with non-numeric vCPU (e.g., db-perf-optimized-N-8)
	tierSpecialRegex = regexp.MustCompile(`^db-([a-z0-9]+)-([a-z0-9]+)-([a-zA-Z]+)-(\d+)$`)
	// Generic regex that matches various db types and tier categories (e.g., standard, highmem, custom, etc.)
	tierGenericRegex = regexp.MustCompile(`^db-([a-z0-9]+)-([a-z0-9]+)-(?:([a-z0-9]+)-)?(\d+)(?:-(\d+))?$`)
)

// Price calculation logic:
// 1. If the instance tier matches the custom format (db-{machine_family}-{tier}-{vCPU}-{RAM}), use custom pricing logic:
//  - Find CPU and RAM component SKUs in the region (usage_unit "h" for CPU, "GiBy" for RAM)
//  - Calculate total price: (vCPU count × CPU price per hour) + (RAM in GB × RAM price per GB per hour)
// 2. If the instance is a standard instance (any other tier format), use standard pricing logic:
//  - Find a SKU matching the region and tier type:
//    * For micro/small tiers (db-f1-micro, db-g1-small): match by tier name in SKU description
//    * For standard tiers (db-n1-standard-*, etc.): match by region only (first matching SKU)
//  - Return the price from the matched SKU

func newPricingMap(logger *slog.Logger, gcpClient client.Client) *pricingMap {
	return &pricingMap{
		logger:    logger,
		gcpClient: gcpClient,
		skus:      []*billingpb.Sku{},
	}
}

func (pm *pricingMap) getSKus(ctx context.Context) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	serviceFullName, err := pm.gcpClient.GetServiceName(ctx, serviceName)
	if err != nil {
		return fmt.Errorf("failed to get service name for Cloud SQL: %w", err)
	}

	skus := pm.gcpClient.GetPricing(ctx, serviceFullName)
	if len(skus) == 0 {
		pm.logger.Warn("no SKUs found for Cloud SQL service", "serviceName", serviceFullName)
		return nil
	}

	pm.skus = skus
	pm.logger.Info("loaded Cloud SQL pricing SKUs", "count", len(skus), "serviceName", serviceFullName)
	return nil
}

func priceForSKU(sku *billingpb.Sku) (float64, bool) {
	if sku == nil {
		return 0, false
	}
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
		tierType := matches[1] + "-" + matches[2]
		return &instanceSpec{
			cpu:      0,
			ram:      0,
			tier:     tier,
			tierType: tierType,
			isCustom: false,
		}, nil
	}

	// Special tiers with non-numeric vCPU (e.g., db-perf-optimized-N-8)
	// For these tiers, we extract the base tier name (e.g., "perf-optimized") without the letter suffix
	// since SKU descriptions typically don't include the letter part
	if matches := tierSpecialRegex.FindStringSubmatch(tier); matches != nil {
		tierType := matches[1] + "-" + matches[2]
		return &instanceSpec{
			cpu:      0,
			ram:      0,
			tier:     tier,
			tierType: tierType,
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

	if it.spec.isCustom {
		return pm.calculateCustomPrice(it.region, it.spec)
	}

	return pm.findStandardInstancePrice(it)
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
	if sku == nil {
		return false
	}
	if len(sku.PricingInfo) == 0 {
		return false
	}
	usageUnit := sku.PricingInfo[0].PricingExpression.UsageUnit
	descLower := strings.ToLower(sku.Description)

	// If a SKU has both vCPU and RAM specs, it's a standard instance SKU
	hasVCPU := strings.Contains(descLower, "vcpu") || strings.Contains(descLower, "cpu")
	hasRAM := strings.Contains(descLower, "ram") || strings.Contains(descLower, "gb ram") || strings.Contains(descLower, "memory")
	if hasVCPU && hasRAM {
		return false
	}

	// CPU component SKU: usage unit is "h" (hour) and description contains CPU-related terms
	isCPU := (usageUnit == "h" || usageUnit == "1/h") &&
		(strings.Contains(descLower, "cpu") || strings.Contains(descLower, "vcpu") || strings.Contains(descLower, "processor"))

	// RAM component SKU: usage unit contains "GiBy" and description contains RAM-related terms
	isRAM := strings.Contains(usageUnit, "GiBy") &&
		(strings.Contains(descLower, "ram") || strings.Contains(descLower, "memory"))

	return isCPU || isRAM
}

func matchByTierType(sku *billingpb.Sku, it instanceTraits) bool {
	descLower := strings.ToLower(sku.Description)
	return it.spec.tierType != "" && strings.Contains(descLower, strings.ToLower(it.spec.tierType))
}

func isSpecialTier(tier string) bool {
	return tierSpecialRegex.MatchString(tier)
}

func matchByTierName(sku *billingpb.Sku, tier string) bool {
	descLower := strings.ToLower(sku.Description)
	tierLower := strings.ToLower(tier)
	// Try matching the tier name (e.g., "db-perf-optimized-n-8")
	// Also try without the "db-" prefix
	tierWithoutPrefix := strings.TrimPrefix(tierLower, "db-")
	return strings.Contains(descLower, tierLower) || strings.Contains(descLower, tierWithoutPrefix)
}

func isStandardSKU(sku *billingpb.Sku, it instanceTraits) bool {
	if sku == nil {
		return false
	}
	if sku.Category == nil || sku.Category.ServiceDisplayName != serviceName {
		return false
	}

	if isCustomPricingSku(sku) {
		return false
	}

	if sku.GeoTaxonomy == nil || len(sku.GeoTaxonomy.Regions) == 0 {
		return false
	}

	if !isSkuInRegion(sku, it.region) {
		return false
	}

	// For special tiers (e.g., db-perf-optimized-N-8), try matching by tier name first
	// If that fails, allow region-only matching (like generic tiers with CPU > 0)
	if isSpecialTier(it.spec.tier) {
		// Try matching by tier name first
		if matchByTierName(sku, it.spec.tier) {
			return true
		}
		// Fall back to region-only matching for special tiers
		return true
	}

	// For other tiers with cpu == 0 (micro/small), require tier type matching
	if it.spec.cpu == 0 {
		if !matchByTierType(sku, it) {
			return false
		}
	}
	return true
}

func (pm *pricingMap) findStandardInstancePrice(it instanceTraits) (*priceMatch, error) {
	for _, sku := range pm.skus {
		if !isStandardSKU(sku, it) {
			continue
		}

		price, _ := priceForSKU(sku)
		return &priceMatch{
			pricePerHour: price,
			skuID:        sku.SkuId,
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

		// custom pricing can be CPU or RAM based. We need to check both.
		isCPU := (usageUnit == "h" || usageUnit == "1/h") &&
			(strings.Contains(descLower, "cpu") || strings.Contains(descLower, "vcpu") || strings.Contains(descLower, "processor"))
		if isCPU && cpuPrice == 0 {
			cpuPrice = price
			cpuSkuID = sku.SkuId
		}

		isRAM := strings.Contains(usageUnit, "GiBy") &&
			(strings.Contains(descLower, "ram") || strings.Contains(descLower, "memory"))
		if isRAM && ramPrice == 0 {
			ramPrice = price
			ramSkuID = sku.SkuId
		}
	}

	if cpuPrice == 0 || ramPrice == 0 {
		return nil, fmt.Errorf("%w: region=%s (cpuPrice=%f, ramPrice=%f)", ErrCustomPriceMissing, region, cpuPrice, ramPrice)
	}

	ramGB := float64(spec.ram) / 1024.0
	totalPrice := float64(spec.cpu)*cpuPrice + ramGB*ramPrice

	return &priceMatch{
		pricePerHour: totalPrice,
		skuID:        fmt.Sprintf("custom-pricing:%s+%s", cpuSkuID, ramSkuID),
		isCustom:     true,
		cpuPrice:     cpuPrice,
		ramPrice:     ramPrice,
	}, nil
}

func isSkuInRegion(sku *billingpb.Sku, region string) bool {
	if sku == nil {
		return false
	}
	if sku.GeoTaxonomy == nil || len(sku.GeoTaxonomy.Regions) == 0 {
		return false
	}
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
