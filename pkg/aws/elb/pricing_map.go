package elb

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"

	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/grafana/cloudcost-exporter/pkg/aws/client"
	"golang.org/x/sync/errgroup"
)

const (
	// Load Balancer Capacity Units (LCU) hourly rate
	NLCUUsageHourlyRateDefault = 0.006
	ALCUUsageHourlyRateDefault = 0.008

	// Load Balancer Usage hourly rate
	LoadBalancerUsageHourlyRateDefault = 0.0225

	// "Used load balancer capacity units (LCU) per hour"
	LCUUsage = "LCUUsage"

	// "LoadBalancer hourly usage by Load Balancer (ALB, NLB) per hour"
	LoadBalancerUsage = "LoadBalancerUsage"
)

type RegionPricing struct {
	ALBHourlyRate map[string]float64
	NLBHourlyRate map[string]float64
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
	}

	prices, err := client.ListELBPrices(ctx, region)
	if err != nil {
		return nil, fmt.Errorf("failed to get ELB pricing: %w", err)
	}

	pricingErr := fmt.Sprintf("error fetching pricing: %s", region)
	for _, product := range prices {
		var productInfo elbProduct
		if err := json.Unmarshal([]byte(product), &productInfo); err != nil {
			pm.logger.Warn(pricingErr, "failed to unmarshal pricing product", err)
			continue
		}

		// Extract pricing information
		for _, term := range productInfo.Terms.OnDemand {
			for _, priceDimension := range term.PriceDimensions {
				price, err := strconv.ParseFloat(priceDimension.PricePerUnit["USD"], 64)
				if err != nil {
					pm.logger.Warn(pricingErr, "failed to parse price", err)
					continue
				}

				operation := productInfo.Product.Attributes.Operation

				// skip pricing for Classic Load Balancers
				if operation == "LoadBalancing" {
					continue
				}

				unit := productInfo.Product.Attributes.UsageType
				if strings.Contains(unit, LCUUsage) {
					unit = LCUUsage
				} else if strings.Contains(unit, LoadBalancerUsage) {
					unit = LoadBalancerUsage
				} else if strings.Contains(unit, "DataProcessing-Bytes") || strings.Contains(unit, "IdleProvisionedLBCapacity") {
					continue
				} else {
					pm.logger.Warn(pricingErr, "unknown usage type", unit)
					continue
				}

				// Determine the load balancer type based on the attribute "operation"
				switch operation {
				case "LoadBalancing:Application":
					regionPricing.ALBHourlyRate[unit] = price
				case "LoadBalancing:Network":
					regionPricing.NLBHourlyRate[unit] = price
				default:
					pm.logger.Warn(pricingErr, "unknown operation", productInfo.Product.Attributes.Operation)
				}
			}
		}
	}

	return regionPricing, nil
}
