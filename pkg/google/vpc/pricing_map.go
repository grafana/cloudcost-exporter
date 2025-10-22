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

// Usage type patterns for GCP VPC services
const (
	CloudNATGatewayPattern        = "Cloud NAT Gateway"
	CloudNATDataProcessingPattern = "Cloud NAT"
	VPNGatewayPattern             = "VPN Gateway"
	PrivateServiceConnectPattern  = "Private Service Connect"
	ExternalIPStaticPattern       = "Static IP"
	ExternalIPEphemeralPattern    = "Ephemeral IP"
	CloudRouterPattern            = "Cloud Router"
)

// VPCRegionPricing holds pricing data for all VPC services in a specific region
type VPCRegionPricing struct {
	CloudNATGatewayRates        map[string]float64
	CloudNATDataProcessingRates map[string]float64
	VPNGatewayRates             map[string]float64
	PrivateServiceConnectRates  map[string]float64
	ExternalIPStaticRates       map[string]float64
	ExternalIPEphemeralRates    map[string]float64
	CloudRouterRates            map[string]float64
}

// NewVPCRegionPricing creates a new VPCRegionPricing instance
func NewVPCRegionPricing() *VPCRegionPricing {
	return &VPCRegionPricing{
		CloudNATGatewayRates:        make(map[string]float64),
		CloudNATDataProcessingRates: make(map[string]float64),
		VPNGatewayRates:             make(map[string]float64),
		PrivateServiceConnectRates:  make(map[string]float64),
		ExternalIPStaticRates:       make(map[string]float64),
		ExternalIPEphemeralRates:    make(map[string]float64),
		CloudRouterRates:            make(map[string]float64),
	}
}

// VPCPricingMap manages pricing data for all GCP VPC services across regions
type VPCPricingMap struct {
	regionPricing map[string]*VPCRegionPricing
	gcpClient     client.Client
	logger        *slog.Logger
	mu            sync.RWMutex
}

// NewVPCPricingMap creates a new VPCPricingMap instance
func NewVPCPricingMap(logger *slog.Logger, gcpClient client.Client) *VPCPricingMap {
	return &VPCPricingMap{
		regionPricing: make(map[string]*VPCRegionPricing),
		gcpClient:     gcpClient,
		logger:        logger,
	}
}

// usageTypeMatcher defines patterns for matching GCP VPC service usage types
type usageTypeMatcher struct {
	patterns []string
}

// Refresh fetches and updates pricing data for all VPC services
func (pm *VPCPricingMap) Refresh(ctx context.Context) error {
	pm.logger.LogAttrs(ctx, slog.LevelInfo, "Refreshing VPC pricing data")

	// Get service names for VPC-related services
	services := []string{"Compute Engine", "Networking"}

	for _, serviceName := range services {
		if err := pm.fetchServicePricing(ctx, serviceName); err != nil {
			pm.logger.Error("Failed to fetch pricing for service", "service", serviceName, "error", err)
			// Continue with other services even if one fails
		}
	}

	return nil
}

// fetchServicePricing fetches pricing data for a specific GCP service
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

// processSKUs processes SKU data and updates the pricing map
func (pm *VPCPricingMap) processSKUs(skus []*billingpb.Sku) error {
	for _, sku := range skus {
		if err := pm.processSingleSKU(sku); err != nil {
			pm.logger.Warn("Failed to process SKU", "sku_id", sku.SkuId, "error", err)
			continue
		}
	}
	return nil
}

// processSingleSKU processes a single SKU and adds it to the appropriate pricing map
func (pm *VPCPricingMap) processSingleSKU(sku *billingpb.Sku) error {
	// Skip SKUs without pricing information
	if len(sku.PricingInfo) == 0 ||
		len(sku.PricingInfo[0].PricingExpression.TieredRates) == 0 ||
		len(sku.GeoTaxonomy.Regions) == 0 {
		return nil
	}

	// Extract pricing information
	region := sku.GeoTaxonomy.Regions[0]
	priceNanos := sku.PricingInfo[0].PricingExpression.TieredRates[0].UnitPrice.Nanos
	price := float64(priceNanos) / 1e9

	// Skip zero or negative prices (invalid data)
	if price <= 0 {
		return nil
	}

	description := sku.Description
	usageType := ""
	if sku.Category != nil && sku.Category.UsageType != "" {
		usageType = sku.Category.UsageType
	}

	// Categorize by service type and add to appropriate pricing map
	return pm.categorizeAndStore(region, description, usageType, price)
}

// categorizeAndStore categorizes the pricing data and stores it in the appropriate map
func (pm *VPCPricingMap) categorizeAndStore(region, description, usageType string, price float64) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Ensure region pricing exists
	if pm.regionPricing[region] == nil {
		pm.regionPricing[region] = NewVPCRegionPricing()
	}

	regionPricing := pm.regionPricing[region]

	// Categorize based on description and usage type
	switch {
	case strings.Contains(description, CloudNATGatewayPattern) || strings.Contains(usageType, "NAT"):
		// Check if this is data processing or gateway hourly pricing
		// Be specific about data processing patterns to avoid false matches
		if strings.Contains(description, "Data Processing") ||
			strings.Contains(description, "Data Processed") ||
			strings.Contains(strings.ToLower(usageType), "dataprocessed") {
			regionPricing.CloudNATDataProcessingRates[usageType] = price
			pm.logger.Debug("Added Cloud NAT data processing pricing", "region", region, "usage_type", usageType, "price", price)
		} else {
			regionPricing.CloudNATGatewayRates[usageType] = price
			pm.logger.Debug("Added Cloud NAT Gateway pricing", "region", region, "usage_type", usageType, "price", price)
		}

	case strings.Contains(description, VPNGatewayPattern) || strings.Contains(usageType, "VPN"):
		regionPricing.VPNGatewayRates[usageType] = price
		pm.logger.Debug("Added VPN Gateway pricing", "region", region, "usage_type", usageType, "price", price)

	case strings.Contains(description, PrivateServiceConnectPattern) || strings.Contains(usageType, "PSC"):
		regionPricing.PrivateServiceConnectRates[usageType] = price
		pm.logger.Debug("Added Private Service Connect pricing", "region", region, "usage_type", usageType, "price", price)

	case strings.Contains(description, ExternalIPStaticPattern) || strings.Contains(usageType, "Static"):
		regionPricing.ExternalIPStaticRates[usageType] = price
		pm.logger.Debug("Added static external IP pricing", "region", region, "usage_type", usageType, "price", price)

	case strings.Contains(description, ExternalIPEphemeralPattern) || strings.Contains(usageType, "Ephemeral"):
		regionPricing.ExternalIPEphemeralRates[usageType] = price
		pm.logger.Debug("Added ephemeral external IP pricing", "region", region, "usage_type", usageType, "price", price)

	case strings.Contains(description, CloudRouterPattern) || strings.Contains(usageType, "Router"):
		regionPricing.CloudRouterRates[usageType] = price
		pm.logger.Debug("Added Cloud Router pricing", "region", region, "usage_type", usageType, "price", price)
	}

	return nil
}

// GetRegionPricing returns pricing data for a specific region
func (pm *VPCPricingMap) GetRegionPricing(region string) (*VPCRegionPricing, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	pricing, exists := pm.regionPricing[region]
	if !exists {
		return nil, fmt.Errorf("no pricing data available for region %s", region)
	}

	return pricing, nil
}

// findRateInMap searches for a rate in the given rate map using the matcher
func (pm *VPCPricingMap) findRateInMap(region string, rates map[string]float64, matcher usageTypeMatcher, serviceType string) (float64, error) {
	for _, pattern := range matcher.patterns {
		for usageType, rate := range rates {
			if strings.Contains(usageType, pattern) {
				pm.logger.Debug("Found rate", "region", region, "service", serviceType, "usage_type", usageType, "rate", rate)
				return rate, nil
			}
		}
	}

	// If no specific pattern matches, return the first available rate
	for usageType, rate := range rates {
		pm.logger.Debug("Using fallback rate", "region", region, "service", serviceType, "usage_type", usageType, "rate", rate)
		return rate, nil
	}

	return 0, fmt.Errorf("no %s pricing found for region %s", serviceType, region)
}

// getRate is a generic helper function to get rates for different VPC services
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
		patterns: []string{"Gateway", "NAT"},
	}
	return pm.getRate(region, "Cloud NAT Gateway", func(p *VPCRegionPricing) map[string]float64 {
		return p.CloudNATGatewayRates
	}, matcher)
}

// GetCloudNATDataProcessingRate returns the data processing rate for Cloud NAT Gateway in the specified region
func (pm *VPCPricingMap) GetCloudNATDataProcessingRate(region string) (float64, error) {
	matcher := usageTypeMatcher{
		patterns: []string{"Data", "Processing"},
	}
	return pm.getRate(region, "Cloud NAT Data Processing", func(p *VPCRegionPricing) map[string]float64 {
		return p.CloudNATDataProcessingRates
	}, matcher)
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

// GetPrivateServiceConnectHourlyRate returns the hourly rate for Private Service Connect in the specified region
func (pm *VPCPricingMap) GetPrivateServiceConnectHourlyRate(region string) (float64, error) {
	matcher := usageTypeMatcher{
		patterns: []string{"Connect", "PSC", "Endpoint"},
	}
	return pm.getRate(region, "Private Service Connect", func(p *VPCRegionPricing) map[string]float64 {
		return p.PrivateServiceConnectRates
	}, matcher)
}

// GetExternalIPStaticHourlyRate returns the hourly rate for static external IPs in the specified region
func (pm *VPCPricingMap) GetExternalIPStaticHourlyRate(region string) (float64, error) {
	matcher := usageTypeMatcher{
		patterns: []string{"Static", "IP"},
	}
	return pm.getRate(region, "Static External IP", func(p *VPCRegionPricing) map[string]float64 {
		return p.ExternalIPStaticRates
	}, matcher)
}

// GetExternalIPEphemeralHourlyRate returns the hourly rate for ephemeral external IPs in the specified region
func (pm *VPCPricingMap) GetExternalIPEphemeralHourlyRate(region string) (float64, error) {
	matcher := usageTypeMatcher{
		patterns: []string{"Ephemeral", "IP"},
	}
	return pm.getRate(region, "Ephemeral External IP", func(p *VPCRegionPricing) map[string]float64 {
		return p.ExternalIPEphemeralRates
	}, matcher)
}

// GetCloudRouterHourlyRate returns the hourly rate for Cloud Router in the specified region
func (pm *VPCPricingMap) GetCloudRouterHourlyRate(region string) (float64, error) {
	matcher := usageTypeMatcher{
		patterns: []string{"Router", "BGP"},
	}
	return pm.getRate(region, "Cloud Router", func(p *VPCRegionPricing) map[string]float64 {
		return p.CloudRouterRates
	}, matcher)
}
