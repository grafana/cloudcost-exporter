package vpc

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"cloud.google.com/go/billing/apiv1/billingpb"
	"github.com/grafana/cloudcost-exporter/pkg/google/client"
)

const (
	VPNGatewayPattern = "Cloud VPN"
)

// VPCRegionPricing holds pricing data for all VPC services in a specific region
type VPCRegionPricing struct {
	CloudNATGatewayRates        map[string]float64
	CloudNATDataProcessingRates map[string]float64
	VPNGatewayRates             map[string]float64
}

// VPCGlobalPricing holds global pricing that applies to all regions
type VPCGlobalPricing struct {
	CloudNATGatewayRates        map[string]float64
	CloudNATDataProcessingRates map[string]float64
}

// NewVPCRegionPricing creates a new VPCRegionPricing instance
func NewVPCRegionPricing() *VPCRegionPricing {
	return &VPCRegionPricing{
		CloudNATGatewayRates:        make(map[string]float64),
		CloudNATDataProcessingRates: make(map[string]float64),
		VPNGatewayRates:             make(map[string]float64),
	}
}

// NewVPCGlobalPricing creates a new VPCGlobalPricing instance
func NewVPCGlobalPricing() *VPCGlobalPricing {
	return &VPCGlobalPricing{
		CloudNATGatewayRates:        make(map[string]float64),
		CloudNATDataProcessingRates: make(map[string]float64),
	}
}

// VPCPricingMap manages pricing data for all GCP VPC services across regions
type VPCPricingMap struct {
	regionPricing map[string]*VPCRegionPricing
	globalPricing *VPCGlobalPricing
	gcpClient     client.Client
	logger        *slog.Logger
	mu            sync.RWMutex
}

// NewVPCPricingMap creates a new VPCPricingMap instance
func NewVPCPricingMap(logger *slog.Logger, gcpClient client.Client) *VPCPricingMap {
	return &VPCPricingMap{
		regionPricing: make(map[string]*VPCRegionPricing),
		globalPricing: NewVPCGlobalPricing(),
		gcpClient:     gcpClient,
		logger:        logger,
	}
}

type usageTypeMatcher struct {
	patterns []string
}

// Refresh fetches and updates pricing data for all VPC services
func (pm *VPCPricingMap) Refresh(ctx context.Context) error {
	pm.logger.LogAttrs(ctx, slog.LevelInfo, "Refreshing VPC pricing data")

	services := []string{"Compute Engine", "Networking"}

	for _, serviceName := range services {
		if err := pm.fetchServicePricing(ctx, serviceName); err != nil {
			pm.logger.Error("Failed to fetch pricing for service", "service", serviceName, "error", err)
		}
	}

	return nil
}

func (pm *VPCPricingMap) fetchServicePricing(ctx context.Context, serviceName string) error {
	gcpServiceName, err := pm.gcpClient.GetServiceName(ctx, serviceName)
	if err != nil {
		return fmt.Errorf("failed to get service name for %s: %w", serviceName, err)
	}

	skus := pm.gcpClient.GetPricing(ctx, gcpServiceName)
	if len(skus) == 0 {
		return fmt.Errorf("no SKUs found for service %s", serviceName)
	}

	return pm.processSKUs(skus)
}

func (pm *VPCPricingMap) processSKUs(skus []*billingpb.Sku) error {
	for _, sku := range skus {
		if err := pm.processSingleSKU(sku); err != nil {
			pm.logger.Warn("Failed to process SKU", "sku_id", sku.SkuId, "error", err)
			continue
		}
	}
	return nil
}

func (pm *VPCPricingMap) processSingleSKU(sku *billingpb.Sku) error {
	if len(sku.PricingInfo) == 0 ||
		len(sku.PricingInfo[0].PricingExpression.TieredRates) == 0 ||
		sku.GeoTaxonomy == nil {
		return nil
	}

	priceNanos := sku.PricingInfo[0].PricingExpression.TieredRates[0].UnitPrice.Nanos
	price := float64(priceNanos) / 1e9

	description := sku.Description
	usageType := ""
	if sku.Category != nil && sku.Category.UsageType != "" {
		usageType = sku.Category.UsageType
	}

	// Handle GLOBAL pricing (applies to all regions)
	if len(sku.GeoTaxonomy.Regions) == 0 {
		return pm.categorizeAndStoreGlobal(description, usageType, price)
	}

	// Handle regional pricing
	region := sku.GeoTaxonomy.Regions[0]
	return pm.categorizeAndStore(region, description, usageType, price)
}

func (pm *VPCPricingMap) categorizeAndStoreGlobal(description, usageType string, price float64) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	descLower := strings.ToLower(description)

	if strings.Contains(descLower, "cloud nat") || strings.Contains(descLower, "nat") {
		if strings.Contains(descLower, "data processing") || strings.Contains(descLower, "data processed") {
			pm.globalPricing.CloudNATDataProcessingRates[usageType] = price
			pm.logger.Info("Stored global Cloud NAT data processing pricing", "usage_type", usageType, "price", price)
		} else if strings.Contains(descLower, "gateway") || strings.Contains(descLower, "uptime") {
			pm.globalPricing.CloudNATGatewayRates[usageType] = price
			pm.logger.Info("Stored global Cloud NAT gateway pricing", "usage_type", usageType, "price", price)
		}
	}

	return nil
}

func (pm *VPCPricingMap) categorizeAndStore(region, description, usageType string, price float64) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pm.regionPricing[region] == nil {
		pm.regionPricing[region] = NewVPCRegionPricing()
	}

	regionPricing := pm.regionPricing[region]

	descLower := strings.ToLower(description)

	// Cloud NAT (regional overrides of global pricing, if they exist)
	if strings.Contains(descLower, "cloud nat") || strings.Contains(descLower, "nat") {
		if strings.Contains(descLower, "data processing") || strings.Contains(descLower, "data processed") {
			regionPricing.CloudNATDataProcessingRates[usageType] = price
		} else if strings.Contains(descLower, "gateway") || strings.Contains(descLower, "uptime") {
			regionPricing.CloudNATGatewayRates[usageType] = price
		}
	}

	// VPN Gateway
	if strings.Contains(description, VPNGatewayPattern) || strings.Contains(usageType, "VPN") {
		regionPricing.VPNGatewayRates[usageType] = price
	}

	return nil
}

// GetRegionPricing returns pricing data for a specific region
func (pm *VPCPricingMap) GetRegionPricing(region string) (*VPCRegionPricing, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	pricing, exists := pm.regionPricing[region]
	if !exists {
		availableRegions := make([]string, 0, len(pm.regionPricing))
		for r := range pm.regionPricing {
			availableRegions = append(availableRegions, r)
		}
		pm.logger.Info("Region not found in pricing map", "requested_region", region, "available_regions_count", len(availableRegions), "sample_regions", availableRegions[:min(5, len(availableRegions))])
		return nil, fmt.Errorf("no pricing data available for region %s", region)
	}

	return pricing, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (pm *VPCPricingMap) findRateInMap(region string, rates map[string]float64, matcher usageTypeMatcher, serviceType string) (float64, error) {
	for _, pattern := range matcher.patterns {
		for usageType, rate := range rates {
			if strings.Contains(usageType, pattern) {
				return rate, nil
			}
		}
	}

	if len(rates) > 0 {
		for _, rate := range rates {
			return rate, nil
		}
	}

	return 0, fmt.Errorf("no %s pricing found for region %s", serviceType, region)
}

func (pm *VPCPricingMap) getRate(region string, serviceType string, rateMapGetter func(*VPCRegionPricing) map[string]float64, matcher usageTypeMatcher) (float64, error) {
	pricing, err := pm.GetRegionPricing(region)
	if err != nil {
		return 0, fmt.Errorf("failed to get %s pricing for region %s: %w", serviceType, region, err)
	}

	rates := rateMapGetter(pricing)
	return pm.findRateInMap(region, rates, matcher, serviceType)
}

// GetCloudNATGatewayHourlyRate returns the hourly rate for Cloud NAT Gateway in the specified region
func (pm *VPCPricingMap) GetCloudNATGatewayHourlyRate(region string) (float64, error) {
	matcher := usageTypeMatcher{
		patterns: []string{"OnDemand"},
	}

	// Try regional pricing first
	rate, err := pm.getRate(region, "Cloud NAT Gateway", func(p *VPCRegionPricing) map[string]float64 {
		return p.CloudNATGatewayRates
	}, matcher)

	if err == nil {
		return rate, nil
	}

	// Fallback to global pricing
	return pm.getGlobalRate("Cloud NAT Gateway", pm.globalPricing.CloudNATGatewayRates, matcher)
}

// GetCloudNATDataProcessingRate returns the data processing rate for Cloud NAT in the specified region
func (pm *VPCPricingMap) GetCloudNATDataProcessingRate(region string) (float64, error) {
	matcher := usageTypeMatcher{
		patterns: []string{"OnDemand"},
	}

	// Try regional pricing first
	rate, err := pm.getRate(region, "Cloud NAT Data Processing", func(p *VPCRegionPricing) map[string]float64 {
		return p.CloudNATDataProcessingRates
	}, matcher)

	if err == nil {
		return rate, nil
	}

	// Fallback to global pricing
	return pm.getGlobalRate("Cloud NAT Data Processing", pm.globalPricing.CloudNATDataProcessingRates, matcher)
}

// GetVPNGatewayHourlyRate returns the hourly rate for VPN Gateway in the specified region
func (pm *VPCPricingMap) GetVPNGatewayHourlyRate(region string) (float64, error) {
	matcher := usageTypeMatcher{
		patterns: []string{"Gateway", "VPN"},
	}
	return pm.getRate(region, "VPN Gateway", func(p *VPCRegionPricing) map[string]float64 {
		return p.VPNGatewayRates
	}, matcher)
}

// getGlobalRate returns a rate from global pricing
func (pm *VPCPricingMap) getGlobalRate(serviceType string, rates map[string]float64, matcher usageTypeMatcher) (float64, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	return pm.findRateInMap("global", rates, matcher, serviceType)
}
