package cloudsql

import (
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"

	"cloud.google.com/go/billing/apiv1/billingpb"
	"github.com/grafana/cloudcost-exporter/pkg/google/client"
	sqladmin "google.golang.org/api/sqladmin/v1beta4"
)

var (
	ErrInstanceNotFound      = errors.New("instance not found")
	ErrPriceNotFound         = errors.New("price not found for instance")
	ErrInvalidTier           = errors.New("invalid tier format")
	ErrRegionNotFound        = errors.New("region not found in pricing")
	ErrComponentPriceMissing = errors.New("component pricing (CPU/RAM) not available")
)

// InstanceSpec represents the compute specifications of a database instance
type InstanceSpec struct {
	VCPU     int
	RAMMB    int // 0 means RAM should be matched from pricing data
	Tier     string
	TierType string // standard, highmem, ultramem, etc. (for non-custom tiers)
	IsCustom bool
}

// PriceMatch represents a matched price for an instance
type PriceMatch struct {
	PricePerHour float64
	SKUID        string
	Description  string
	isCustom     bool
	CPUPrice     float64 // per vCPU per hour (if component-based)
	RAMPrice     float64 // per GB per hour (if component-based)
}

type pricingMap struct {
	logger    *slog.Logger
	gcpClient client.Client
	mu        sync.RWMutex
	skus      []*billingpb.Sku
}

type ParsedSkuData struct {
	Region         string
	Price          float64
	ResourceFamily string
}

var (
	serviceName    = "Cloud SQL"
	databaseFamily = "ApplicationServices"

	// Resource groups for compute instances
	computeResourceGroups = map[string]bool{
		"SQLGen2InstancesN1Standard": true,
		"SQLGen2InstancesN1Highmem":  true,
	}

	// Resource groups for component pricing
	componentResourceGroups = map[string]bool{
		"SQLGen2InstancesCPU": true,
		"SQLGen2InstancesRAM": true,
	}

	// Tier parsing regex
	tierCustomRegex = regexp.MustCompile(`^db-custom-(\d+)-(\d+)$`)
	// Generic regex that matches various db types and tier categories (e.g., standard, highmem, custom, etc.)
	tierGenericRegex = regexp.MustCompile(`^db-([a-z0-9]+)-([a-z0-9]+)-(?:([a-z0-9]+)-)?(\d+)(?:-(\d+))?$`)
	// Examples this matches:
	//   db-n1-standard-2
	//   db-n1-highmem-4
	//   db-custom-2-7680
	//   db-m2-ultramem-16
	//   db-postgres-standard-8
	// The capture groups are:
	//   1: machine family/db type (e.g., n1, postgres)
	//   2: tier (e.g., standard, highmem, ultramem, custom)
	//   3: (optional) sub-tier or variant (can be empty), often absent
	//   4: vCPU
	//   5: (optional) RAM in MB, used for custom tiers
)

func newPricingMap(logger *slog.Logger, gcpClient client.Client) *pricingMap {
	return &pricingMap{
		logger:    logger,
		gcpClient: gcpClient,
	}
}

// extractPriceFromSku extracts the price from a SKU's pricing information
func extractPriceFromSku(sku *billingpb.Sku) (float64, bool) {
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

// parseTier extracts vCPU and RAM from tier name
func parseTier(tier string) (*InstanceSpec, error) {
	// Custom tier: db-custom-{vCPU}-{RAM_MB}
	if matches := tierCustomRegex.FindStringSubmatch(tier); matches != nil {
		return &InstanceSpec{
			VCPU:     parseInt(matches[1]),
			RAMMB:    parseInt(matches[2]),
			Tier:     tier,
			IsCustom: true,
		}, nil
	}

	if matches := tierGenericRegex.FindStringSubmatch(tier); matches != nil {
		// Extract vCPU count (group 4 in the regex)
		vcpuStr := matches[4]
		if vcpuStr == "" {
			return nil, fmt.Errorf("%w: could not extract vCPU from tier %s", ErrInvalidTier, tier)
		}
		vcpu := parseInt(vcpuStr)

		// Extract tier type (group 2: standard, highmem, ultramem, etc.)
		tierType := matches[2]

		// For standard tiers, we don't hardcode RAM values.
		// Instead, we set RAMMB to 0 to indicate it should be matched from pricing data.
		// The matching logic will find the appropriate RAM value from actual pricing SKUs.
		// This makes the code future-proof as Google introduces new machine families or changes RAM ratios.
		return &InstanceSpec{
			VCPU:     vcpu,
			RAMMB:    0, // Will be matched from pricing data
			Tier:     tier,
			TierType: tierType, // Store tier type for reference
			IsCustom: false,
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
func extractSpecsFromDescription(description string) (vcpu int, ramMB int, ok bool) {
	descLower := strings.ToLower(description)

	// Extract vCPU
	vcpuRegex := regexp.MustCompile(`(\d+)\s*vcpu`)
	vcpuMatch := vcpuRegex.FindStringSubmatch(descLower)
	if vcpuMatch == nil {
		return 0, 0, false
	}
	vcpu = parseInt(vcpuMatch[1])

	// Extract RAM (handles GB or MB, with decimals)
	ramRegex := regexp.MustCompile(`(\d+\.?\d*)\s*(gb|mb)\s*ram`)
	ramMatch := ramRegex.FindStringSubmatch(descLower)
	if ramMatch == nil {
		return 0, 0, false
	}

	var ramValue float64
	fmt.Sscanf(ramMatch[1], "%f", &ramValue)
	ramUnit := strings.ToLower(ramMatch[2])

	if ramUnit == "gb" {
		ramMB = int(ramValue * 1024)
	} else {
		ramMB = int(ramValue)
	}

	return vcpu, ramMB, true
}

// getDatabaseType extracts database type from version string
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

// getAvailabilityType normalizes availability type
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

// matchInstancePrice matches a database instance with its price
func (pm *pricingMap) matchInstancePrice(instance *sqladmin.DatabaseInstance) (*PriceMatch, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	// Validate and extract instance information
	region, dbType, availability, spec, err := pm.extractInstanceInfo(instance)
	if err != nil {
		return nil, err
	}

	// For standard instances, search through SKUs to find matching price
	if !spec.IsCustom {
		priceMatch, err := pm.findStandardInstancePrice(region, dbType, availability, spec)
		if err == nil {
			return priceMatch, nil
		}
	}

	// For custom configurations, use custom pricing
	if spec.IsCustom {
		return pm.calculateCustomPrice(region, spec)
	}

	return nil, fmt.Errorf("%w: region=%s, dbType=%s, availability=%s, vcpu=%d, ramMB=%d",
		ErrPriceNotFound, region, dbType, availability, spec.VCPU, spec.RAMMB)
}

// extractInstanceInfo validates and extracts information from a database instance
func (pm *pricingMap) extractInstanceInfo(instance *sqladmin.DatabaseInstance) (region, dbType, availability string, spec *InstanceSpec, err error) {
	if instance.Settings == nil || instance.Settings.Tier == "" {
		return "", "", "", nil, fmt.Errorf("instance missing tier information")
	}

	// Parse tier to get specs
	spec, err = parseTier(instance.Settings.Tier)
	if err != nil {
		return "", "", "", nil, err
	}

	// Get database type
	dbType = getDatabaseType(instance.DatabaseVersion)
	if dbType == "" {
		return "", "", "", nil, fmt.Errorf("unknown database type: %s", instance.DatabaseVersion)
	}

	// Get availability
	availability = getAvailabilityType(instance.Settings.AvailabilityType)
	if availability == "" {
		return "", "", "", nil, fmt.Errorf("unknown availability type: %s", instance.Settings.AvailabilityType)
	}

	// Get region
	region = instance.Region
	if region == "" {
		return "", "", "", nil, fmt.Errorf("instance missing region")
	}

	return region, dbType, availability, spec, nil
}

// findStandardInstancePrice searches through SKUs to find matching standard instance price
func (pm *pricingMap) findStandardInstancePrice(region, dbType, availability string, spec *InstanceSpec) (*PriceMatch, error) {
	for sku := range pm.filterSkus(region, computeResourceGroups, true) {
		// Check database type
		skuDbType := dbTypeFromDescription(sku.Description)
		if skuDbType != dbType {
			continue
		}

		// Check availability
		skuAvailability := availabilityFromDescription(sku.Description)
		if skuAvailability != availability {
			continue
		}

		// Extract and match vCPU
		vcpu, _, ok := extractSpecsFromDescription(sku.Description)
		if !ok || vcpu != spec.VCPU {
			continue
		}

		// Extract price
		price, ok := extractPriceFromSku(sku)
		if !ok {
			continue
		}

		return &PriceMatch{
			PricePerHour: price,
			SKUID:        sku.SkuId,
			Description:  sku.Description,
			isCustom:     false,
			CPUPrice:     0,
			RAMPrice:     0,
		}, nil
	}

	return nil, ErrPriceNotFound
}

// calculateCustomPrice calculates price from CPU and RAM components
// Prices vary by region, so we require an exact region match
func (pm *pricingMap) calculateCustomPrice(region string, spec *InstanceSpec) (*PriceMatch, error) {
	var cpuPrice, ramPrice float64
	var cpuSkuID, ramSkuID string

	// Search through SKUs to find CPU and RAM component pricing for the region
	for sku := range pm.filterSkus(region, componentResourceGroups, false) {
		price, ok := extractPriceFromSku(sku)
		if !ok {
			continue
		}

		resourceGroup := sku.Category.ResourceGroup
		usageUnit := sku.PricingInfo[0].PricingExpression.UsageUnit

		// Extract CPU price (per vCPU per hour)
		if resourceGroup == "SQLGen2InstancesCPU" && usageUnit == "h" {
			cpuPrice = price
			cpuSkuID = sku.SkuId
		}

		// Extract RAM price (per GB per hour)
		if resourceGroup == "SQLGen2InstancesRAM" && strings.Contains(usageUnit, "GiBy") {
			ramPrice = price
			ramSkuID = sku.SkuId
		}
	}

	// Verify we have both CPU and RAM prices
	if cpuPrice == 0 || ramPrice == 0 {
		return nil, fmt.Errorf("%w: region=%s (cpuPrice=%f, ramPrice=%f)", ErrComponentPriceMissing, region, cpuPrice, ramPrice)
	}

	// Calculate total price: (vCPU * cpuPrice) + (RAM_GB * ramPrice)
	ramGB := float64(spec.RAMMB) / 1024.0
	totalPrice := float64(spec.VCPU)*cpuPrice + ramGB*ramPrice

	// Use combined SKU ID or create a descriptive one
	skuID := fmt.Sprintf("component-based:%s+%s", cpuSkuID, ramSkuID)

	return &PriceMatch{
		PricePerHour: totalPrice,
		SKUID:        skuID,
		Description:  fmt.Sprintf("Component pricing: %d vCPU + %.2fGB RAM", spec.VCPU, ramGB),
		isCustom:     true,
		CPUPrice:     cpuPrice,
		RAMPrice:     ramPrice,
	}, nil
}

// filterSkus returns a channel of SKUs that match the service, region, and resource groups
func (pm *pricingMap) filterSkus(region string, resourceGroups map[string]bool, checkResourceFamily bool) <-chan *billingpb.Sku {
	ch := make(chan *billingpb.Sku)
	go func() {
		defer close(ch)
		for _, sku := range pm.skus {
			// Check service name
			if sku.Category == nil || sku.Category.ServiceDisplayName != serviceName {
				continue
			}

			// Check resource group
			resourceGroup := sku.Category.ResourceGroup
			if !resourceGroups[resourceGroup] {
				continue
			}

			// Check resource family (for compute instances)
			if checkResourceFamily && sku.Category.ResourceFamily != databaseFamily {
				continue
			}

			// Check region
			if len(sku.GeoTaxonomy.Regions) == 0 || !isSkuInRegion(sku, region) {
				continue
			}

			ch <- sku
		}
	}()
	return ch
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
