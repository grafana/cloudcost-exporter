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
	// Default hourly rates (fallback values)
	VPCEndpointHourlyRateDefault    = 0.01  // $0.01 per VPC endpoint per hour
	TransitGatewayHourlyRateDefault = 0.05  // $0.05 per transit gateway attachment per hour
	ElasticIPInUseRateDefault       = 0.005 // $0.005 per in-use elastic IP per hour
	ElasticIPIdleRateDefault        = 0.005 // $0.005 per idle elastic IP per hour

	// Usage type categories for parsing
	VPCEndpointUsagePattern    = "VpcEndpoint"
	TransitGatewayUsagePattern = "TransitGateway"
	ElasticIPUsagePattern      = "PublicIPv4"
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

// Refresh fetches and updates pricing data for all regions
func (pm *VPCPricingMap) Refresh(ctx context.Context, regions []ec2Types.Region, clients map[string]client.Client) error {
	pm.logger.Info("Refreshing VPC pricing data")

	eg := errgroup.Group{}
	var mu sync.Mutex

	for _, region := range regions {
		regionName := *region.RegionName
		regionClient, ok := clients[regionName]
		if !ok {
			pm.logger.Warn("No client found for region", "region", regionName)
			continue
		}

		eg.Go(func() error {
			pricing, err := pm.FetchRegionPricing(regionClient, ctx, regionName)
			if err != nil {
				return fmt.Errorf("failed to fetch VPC pricing for region %s: %w", regionName, err)
			}

			mu.Lock()
			pm.SetRegionPricing(regionName, pricing)
			mu.Unlock()

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

		usageType := productInfo.Product.Attributes.UsageType
		if usageType == "" {
			continue
		}

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

				// Skip zero prices
				if price == 0 {
					continue
				}

				// Categorize by usage type patterns
				if strings.Contains(usageType, VPCEndpointUsagePattern) {
					regionPricing.VPCEndpointRates[usageType] = price
					pm.logger.Debug("Added VPC Endpoint pricing",
						"region", region, "usageType", usageType, "price", price)
				} else if strings.Contains(usageType, TransitGatewayUsagePattern) {
					regionPricing.TransitGatewayRates[usageType] = price
					pm.logger.Debug("Added Transit Gateway pricing",
						"region", region, "usageType", usageType, "price", price)
				} else if strings.Contains(usageType, ElasticIPUsagePattern) {
					regionPricing.ElasticIPRates[usageType] = price
					pm.logger.Debug("Added Elastic IP pricing",
						"region", region, "usageType", usageType, "price", price)
				} else {
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

// GetVPCEndpointHourlyRate returns the hourly rate for standard VPC endpoints in a region
func (pm *VPCPricingMap) GetVPCEndpointHourlyRate(region string) float64 {
	pricing, err := pm.GetRegionPricing(region)
	if err != nil {
		pm.logger.Warn("Failed to get VPC endpoint pricing", "region", region, "error", err)
		return VPCEndpointHourlyRateDefault
	}

	// Look for standard VPC endpoint hourly rates (VpcEndpoint-Hours)
	for usageType, rate := range pricing.VPCEndpointRates {
		if strings.Contains(usageType, "VpcEndpoint-Hours") && !strings.Contains(usageType, "Service") {
			pm.logger.Info("Selected standard VPC endpoint rate", "region", region, "usageType", usageType, "rate", rate)
			return rate
		}
	}

	// Fallback to any VPC endpoint hourly rate (excluding service-specific)
	for usageType, rate := range pricing.VPCEndpointRates {
		if strings.Contains(usageType, "Hours") && !strings.Contains(usageType, "Service") {
			pm.logger.Debug("Selected fallback standard VPC endpoint rate", "region", region, "usageType", usageType, "rate", rate)
			return rate
		}
	}

	pm.logger.Warn("No standard VPC endpoint hourly rate found", "region", region)
	return VPCEndpointHourlyRateDefault
}

// GetVPCServiceEndpointHourlyRate returns the hourly rate for VPC service endpoints in a region
func (pm *VPCPricingMap) GetVPCServiceEndpointHourlyRate(region string) float64 {
	pricing, err := pm.GetRegionPricing(region)
	if err != nil {
		pm.logger.Warn("Failed to get VPC service endpoint pricing", "region", region, "error", err)
		return VPCEndpointHourlyRateDefault
	}

	// Look for VPC service endpoint hourly rates (VpcEndpoint-Service-Hours)
	for usageType, rate := range pricing.VPCEndpointRates {
		if strings.Contains(usageType, "VpcEndpoint-Service-Hours") {
			pm.logger.Info("Selected VPC service endpoint rate", "region", region, "usageType", usageType, "rate", rate)
			return rate
		}
	}

	// Fallback to any service-related VPC endpoint rate
	for usageType, rate := range pricing.VPCEndpointRates {
		if strings.Contains(usageType, "Service") && strings.Contains(usageType, "Hours") {
			pm.logger.Debug("Selected fallback VPC service endpoint rate", "region", region, "usageType", usageType, "rate", rate)
			return rate
		}
	}

	pm.logger.Warn("No VPC service endpoint hourly rate found", "region", region)
	return VPCEndpointHourlyRateDefault
}

// GetTransitGatewayHourlyRate returns the hourly rate for Transit Gateway in a region
func (pm *VPCPricingMap) GetTransitGatewayHourlyRate(region string) float64 {
	pricing, err := pm.GetRegionPricing(region)
	if err != nil {
		pm.logger.Warn("Failed to get Transit Gateway pricing", "region", region, "error", err)
		return TransitGatewayHourlyRateDefault
	}

	// Look for Transit Gateway hourly rates
	for usageType, rate := range pricing.TransitGatewayRates {
		if strings.Contains(usageType, "Hours") {
			return rate
		}
	}

	pm.logger.Warn("No Transit Gateway hourly rate found", "region", region)
	return TransitGatewayHourlyRateDefault
}

// GetElasticIPInUseRate returns the hourly rate for in-use Elastic IPs in a region
func (pm *VPCPricingMap) GetElasticIPInUseRate(region string) float64 {
	pricing, err := pm.GetRegionPricing(region)
	if err != nil {
		pm.logger.Warn("Failed to get Elastic IP pricing", "region", region, "error", err)
		return ElasticIPInUseRateDefault
	}

	// Look for in-use Elastic IP rates
	for usageType, rate := range pricing.ElasticIPRates {
		if strings.Contains(usageType, "InUseAddress") {
			return rate
		}
	}

	pm.logger.Warn("No Elastic IP in-use rate found", "region", region)
	return ElasticIPInUseRateDefault
}

// GetElasticIPIdleRate returns the hourly rate for idle Elastic IPs in a region
func (pm *VPCPricingMap) GetElasticIPIdleRate(region string) float64 {
	pricing, err := pm.GetRegionPricing(region)
	if err != nil {
		pm.logger.Warn("Failed to get Elastic IP pricing", "region", region, "error", err)
		return ElasticIPIdleRateDefault
	}

	// Look for idle Elastic IP rates
	for usageType, rate := range pricing.ElasticIPRates {
		if strings.Contains(usageType, "IdleAddress") {
			return rate
		}
	}

	pm.logger.Warn("No Elastic IP idle rate found", "region", region)
	return ElasticIPIdleRateDefault
}
