package elb

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"sync"

	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/grafana/cloudcost-exporter/pkg/aws/client"
	"golang.org/x/sync/errgroup"
)

const (
	ALBHourlyRateDefault = 0.0225
	NLBHourlyRateDefault = 0.0225
	CLBHourlyRateDefault = 0.025
)

type RegionPricing struct {
	ALBHourlyRate map[string]float64
	NLBHourlyRate map[string]float64
	CLBHourlyRate map[string]float64
}

type ELBPricingMap struct {
	mu      sync.RWMutex
	pricing map[string]*RegionPricing
	logger  *slog.Logger
}

func NewELBPricingMap(logger *slog.Logger) *ELBPricingMap {
	return &ELBPricingMap{
		pricing: make(map[string]*RegionPricing),
		logger:  logger,
	}
}

func (pm *ELBPricingMap) SetRegionPricing(region string, pricing *RegionPricing) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.pricing[region] = pricing
}

func (pm *ELBPricingMap) GetRegionPricing(region string) (*RegionPricing, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	pricing, err := pm.pricing[region]
	if !err {
		return nil, fmt.Errorf("region pricing not found for region: %s", region)
	}
	return pricing, nil
}

func (pm *ELBPricingMap) refresh(client map[string]client.Client, regions []ec2Types.Region) error {
	pm.logger.Info("Refreshing ELB pricing data")

	eg := errgroup.Group{}
	var mu sync.Mutex

	for _, region := range regions {
		regionName := *region.RegionName
		eg.Go(func() error {
			pricing, err := pm.FetchRegionPricing(client[regionName], context.Background(), regionName)
			if err != nil {
				return fmt.Errorf("failed to fetch pricing for region %s: %w", regionName, err)
			}

			mu.Lock()
			pm.SetRegionPricing(regionName, pricing)
			mu.Unlock()

			return nil
		})
	}

	return eg.Wait()
}

func (pm *ELBPricingMap) FetchRegionPricing(client client.Client, ctx context.Context, region string) (*RegionPricing, error) {
	regionPricing := &RegionPricing{
		ALBHourlyRate: make(map[string]float64),
		NLBHourlyRate: make(map[string]float64),
		CLBHourlyRate: make(map[string]float64),
	}

	prices, err := client.ListELBPrices(ctx, region)
	if err != nil {
		return nil, fmt.Errorf("failed to get ELB pricing: %w", err)
	}

	for _, product := range prices {
		var productInfo elbProduct
		if err := json.Unmarshal([]byte(product), &productInfo); err != nil {
			pm.logger.Warn("Failed to unmarshal pricing product", "error", err)
			continue
		}

		// Extract pricing information
		for _, term := range productInfo.Terms.OnDemand {
			for _, priceDimension := range term.PriceDimensions {
				price, err := strconv.ParseFloat(priceDimension.PricePerUnit["USD"], 64)
				if err != nil {
					continue
				}

				// Determine the load balancer type based on product family or attributes
				switch productInfo.Product.Attributes.ProductFamily {
				case "Load Balancer-Application":
					regionPricing.ALBHourlyRate["default"] = price
				case "Load Balancer-Network":
					regionPricing.NLBHourlyRate["default"] = price
				case "Load Balancer":
					// Classic Load Balancer
					regionPricing.CLBHourlyRate["default"] = price
				}
			}
		}
	}

	// Set default rates if not found (fallback values)
	if len(regionPricing.ALBHourlyRate) == 0 {
		pm.logger.Warn("No ALB pricing data available for region", "region", region)
		regionPricing.ALBHourlyRate["default"] = ALBHourlyRateDefault // Default ALB rate
	}
	if len(regionPricing.NLBHourlyRate) == 0 {
		pm.logger.Warn("No NLB pricing data available for region", "region", region)
		regionPricing.NLBHourlyRate["default"] = NLBHourlyRateDefault // Default NLB rate
	}
	if len(regionPricing.CLBHourlyRate) == 0 {
		pm.logger.Warn("No CLB pricing data available for region", "region", region)
		regionPricing.CLBHourlyRate["default"] = CLBHourlyRateDefault // Default CLB rate
	}

	return regionPricing, nil
}
