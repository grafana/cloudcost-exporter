package vpc

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"

	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	pricingTypes "github.com/aws/aws-sdk-go-v2/service/pricing/types"
	"github.com/grafana/cloudcost-exporter/pkg/aws/client"
	"golang.org/x/sync/errgroup"
)

const (
	// Usage type categories for parsing
	VPCEndpointUsagePattern    = "VpcEndpoint"
	TransitGatewayUsagePattern = "TransitGateway"
	ElasticIPUsagePattern      = "PublicIPv4"

	// Specific usage type patterns for pricing lookups
	VpcEndpointHours        = "VpcEndpoint-Hours"
	VpcEndpointServiceHours = "VpcEndpoint-Service-Hours"
	TransitGatewayHours     = "Hours"
	ElasticIPInUseAddress   = "InUseAddress"
	ElasticIPIdleAddress    = "IdleAddress"
	ServicePattern          = "Service"
)

// VPCRegionPricing holds all VPC pricing data for a specific region
type VPCRegionPricing struct {
	VPCEndpointRates    map[string]float64 // VPC Endpoint hourly rates by usage type
	TransitGatewayRates map[string]float64 // Transit Gateway hourly rates by usage type
	ElasticIPRates      map[string]float64 // Elastic IP hourly rates by usage type
}

// VPCPricingMap manages VPC pricing data across all regions
type VPCPricingMap struct {
	mu      sync.RWMutex
	pricing map[string]*VPCRegionPricing // region -> pricing data
	logger  *slog.Logger
}

// vpcProduct represents the structure of VPC pricing API response
type vpcProduct struct {
	Product struct {
		Attributes struct {
			RegionCode string `json:"regionCode"`
			UsageType  string `json:"usageType"`
			Operation  string `json:"operation"`
		} `json:"attributes"`
	} `json:"product"`
	Terms struct {
		OnDemand map[string]struct {
			PriceDimensions map[string]struct {
				PricePerUnit map[string]string `json:"pricePerUnit"`
				Description  string            `json:"description"`
			} `json:"priceDimensions"`
		} `json:"OnDemand"`
	} `json:"terms"`
}

// NewVPCPricingMap creates a new VPC pricing map
func NewVPCPricingMap(logger *slog.Logger) *VPCPricingMap {
	return &VPCPricingMap{
		pricing: make(map[string]*VPCRegionPricing),
		logger:  logger,
	}
}

// SetRegionPricing sets the pricing data for a specific region
func (pm *VPCPricingMap) SetRegionPricing(region string, pricing *VPCRegionPricing) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.pricing[region] = pricing
}

// GetRegionPricing retrieves the pricing data for a specific region
func (pm *VPCPricingMap) GetRegionPricing(region string) (*VPCRegionPricing, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	pricing, exists := pm.pricing[region]
	if !exists {
		return nil, fmt.Errorf("pricing data not found for region: %s", region)
	}
	return pricing, nil
}

// Refresh fetches and updates pricing data for all regions using a single dedicated client
func (pm *VPCPricingMap) Refresh(ctx context.Context, regions []ec2Types.Region, client client.Client) error {
	pm.logger.Info("Refreshing VPC pricing data")

	eg := errgroup.Group{}

	for _, region := range regions {
		regionName := *region.RegionName

		eg.Go(func() error {
			pricing, err := pm.FetchRegionPricing(client, ctx, regionName)
			if err != nil {
				return fmt.Errorf("failed to fetch VPC pricing for region %s: %w", regionName, err)
			}

			pm.SetRegionPricing(regionName, pricing)

			return nil
		})
	}

	return eg.Wait()
}

// FetchRegionPricing fetches VPC pricing data for a specific region
func (pm *VPCPricingMap) FetchRegionPricing(client client.Client, ctx context.Context, region string) (*VPCRegionPricing, error) {
	regionPricing := &VPCRegionPricing{
		VPCEndpointRates:    make(map[string]float64),
		TransitGatewayRates: make(map[string]float64),
		ElasticIPRates:      make(map[string]float64),
	}

	// Fetch all VPC pricing data with no filters (gets everything from AmazonVPC service)
	prices, err := client.ListVPCServicePrices(ctx, region, []pricingTypes.Filter{})
	if err != nil {
		return nil, fmt.Errorf("failed to get VPC pricing: %w", err)
	}

	pricingErr := fmt.Sprintf("error fetching VPC pricing for region %s", region)

	for _, product := range prices {
		var productInfo vpcProduct
		if err := json.Unmarshal([]byte(product), &productInfo); err != nil {
			pm.logger.Warn(pricingErr, "error", "failed to unmarshal pricing product", "unmarshal_error", err)
			continue
		}

		// Check for nil pointer dereference and empty fields
		if productInfo.Product.Attributes.UsageType == "" ||
			productInfo.Product.Attributes.RegionCode == "" {
			continue
		}
		usageType := productInfo.Product.Attributes.UsageType

		// Extract pricing information
		for _, term := range productInfo.Terms.OnDemand {
			for _, priceDimension := range term.PriceDimensions {
				priceStr, exists := priceDimension.PricePerUnit["USD"]
				if !exists {
					continue
				}

				price, err := strconv.ParseFloat(priceStr, 64)
				if err != nil {
					pm.logger.Warn(pricingErr, "error", "failed to parse price", "price", priceStr, "parse_error", err)
					continue
				}

				// Categorize by usage type patterns using switch for better readability
				switch {
				case strings.Contains(usageType, VPCEndpointUsagePattern):
					regionPricing.VPCEndpointRates[usageType] = price
					pm.logger.Debug("Added VPC Endpoint pricing",
						"region", region, "usageType", usageType, "price", price)
				case strings.Contains(usageType, TransitGatewayUsagePattern):
					regionPricing.TransitGatewayRates[usageType] = price
					pm.logger.Debug("Added Transit Gateway pricing",
						"region", region, "usageType", usageType, "price", price)
				case strings.Contains(usageType, ElasticIPUsagePattern):
					regionPricing.ElasticIPRates[usageType] = price
					pm.logger.Debug("Added Elastic IP pricing",
						"region", region, "usageType", usageType, "price", price)
				default:
					pm.logger.Debug("Skipped unknown VPC usage type",
						"region", region, "usageType", usageType)
				}
			}
		}
	}

	pm.logger.Info("Fetched VPC pricing data",
		"region", region,
		"vpcEndpoints", len(regionPricing.VPCEndpointRates),
		"transitGateways", len(regionPricing.TransitGatewayRates),
		"elasticIPs", len(regionPricing.ElasticIPRates))

	return regionPricing, nil
}

// usageTypeMatcher defines how to match usage types for pricing
type usageTypeMatcher struct {
	patterns        []string // Patterns to match (in order of preference)
	excludePatterns []string // Patterns to exclude
}

// findRateInMap searches for a rate in the given rates map using the matcher criteria
func (pm *VPCPricingMap) findRateInMap(region string, rates map[string]float64, matcher usageTypeMatcher, serviceType string) (float64, error) {
	for _, pattern := range matcher.patterns {
		for usageType, rate := range rates {
			if strings.Contains(usageType, pattern) {
				// Check if we should exclude this usage type
				excluded := false
				for _, excludePattern := range matcher.excludePatterns {
					if strings.Contains(usageType, excludePattern) {
						excluded = true
						break
					}
				}
				if !excluded {
					pm.logger.Info("Selected "+serviceType+" rate", "region", region, "usageType", usageType, "rate", rate, "pattern", pattern)
					return rate, nil
				}
			}
		}
	}

	return 0, fmt.Errorf("no %s rate found for region %s", serviceType, region)
}

// getRate is a generic helper for getting pricing rates
func (pm *VPCPricingMap) getRate(region string, serviceType string, rateMapGetter func(*VPCRegionPricing) map[string]float64, matcher usageTypeMatcher) (float64, error) {
	pricing, err := pm.GetRegionPricing(region)
	if err != nil {
		return 0, fmt.Errorf("failed to get %s pricing for region %s: %w", serviceType, region, err)
	}

	rates := rateMapGetter(pricing)
	return pm.findRateInMap(region, rates, matcher, serviceType)
}

// GetVPCEndpointHourlyRate returns the hourly rate for standard VPC endpoints in a region
func (pm *VPCPricingMap) GetVPCEndpointHourlyRate(region string) (float64, error) {
	matcher := usageTypeMatcher{
		patterns:        []string{VpcEndpointHours},
		excludePatterns: []string{ServicePattern},
	}
	return pm.getRate(region, "standard VPC endpoint", func(p *VPCRegionPricing) map[string]float64 {
		return p.VPCEndpointRates
	}, matcher)
}

// GetVPCServiceEndpointHourlyRate returns the hourly rate for VPC service endpoints in a region
func (pm *VPCPricingMap) GetVPCServiceEndpointHourlyRate(region string) (float64, error) {
	matcher := usageTypeMatcher{
		patterns:        []string{VpcEndpointServiceHours, ServicePattern},
		excludePatterns: []string{},
	}
	return pm.getRate(region, "VPC service endpoint", func(p *VPCRegionPricing) map[string]float64 {
		return p.VPCEndpointRates
	}, matcher)
}

// GetTransitGatewayHourlyRate returns the hourly rate for Transit Gateway in a region
func (pm *VPCPricingMap) GetTransitGatewayHourlyRate(region string) (float64, error) {
	matcher := usageTypeMatcher{
		patterns:        []string{TransitGatewayHours},
		excludePatterns: []string{},
	}
	return pm.getRate(region, "Transit Gateway", func(p *VPCRegionPricing) map[string]float64 {
		return p.TransitGatewayRates
	}, matcher)
}

// GetElasticIPInUseRate returns the hourly rate for in-use Elastic IPs in a region
func (pm *VPCPricingMap) GetElasticIPInUseRate(region string) (float64, error) {
	matcher := usageTypeMatcher{
		patterns:        []string{ElasticIPInUseAddress},
		excludePatterns: []string{},
	}
	return pm.getRate(region, "Elastic IP in-use", func(p *VPCRegionPricing) map[string]float64 {
		return p.ElasticIPRates
	}, matcher)
}

// GetElasticIPIdleRate returns the hourly rate for idle Elastic IPs in a region
func (pm *VPCPricingMap) GetElasticIPIdleRate(region string) (float64, error) {
	matcher := usageTypeMatcher{
		patterns:        []string{ElasticIPIdleAddress},
		excludePatterns: []string{},
	}
	return pm.getRate(region, "Elastic IP idle", func(p *VPCRegionPricing) map[string]float64 {
		return p.ElasticIPRates
	}, matcher)
}
