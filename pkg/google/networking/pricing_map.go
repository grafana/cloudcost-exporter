package networking

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"cloud.google.com/go/billing/apiv1/billingpb"
	"github.com/grafana/cloudcost-exporter/pkg/google/client"
)

var (
	forwardingRuleDescription        = "Forwarding Rule"
	inboundDataProcessedDescription  = "Inbound Data Processing"
	outboundDataProcessedDescription = "Outbound Data Processing"
	serviceName                      = "Networking"
	ResourceGroup                    = "LoadBalancing"

	ErrRefreshingPricingMap            = errors.New("error refreshing pricing map")
	ErrGettingNetworkingServiceName    = errors.New("error getting networking service name")
	ErrNoSKUsFoundForNetworkingService = errors.New("no skus found for networking service")
	ErrParsingSKUs                     = errors.New("error parsing skus")
	ErrRegionNotFound                  = errors.New("region not found")
	ErrUnknownDescription              = errors.New("unknown description")
)

type ParsedSkuData struct {
	Region      string
	Price       float64
	Description string
}

func NewParsedSkuData(region string, price float64, description string) *ParsedSkuData {
	return &ParsedSkuData{
		Region:      region,
		Price:       price,
		Description: description,
	}
}

type pricing struct {
	forwardingRuleCost        float64
	inboundDataProcessedCost  float64
	outboundDataProcessedCost float64
}

func NewPricing() *pricing {
	return &pricing{
		forwardingRuleCost:        0,
		inboundDataProcessedCost:  0,
		outboundDataProcessedCost: 0,
	}
}

type pricingMap struct {
	pricing   map[string]*pricing
	logger    *slog.Logger
	gcpClient client.Client
	mu        sync.RWMutex
}

func newPricingMap(logger *slog.Logger, gcpClient client.Client) (*pricingMap, error) {
	pm := &pricingMap{
		pricing:   make(map[string]*pricing),
		logger:    logger,
		gcpClient: gcpClient,
	}

	if err := pm.populate(context.Background()); err != nil {
		return nil, err
	}
	return pm, nil
}

func (pm *pricingMap) getServiceName(ctx context.Context) (string, error) {
	serviceName, err := pm.gcpClient.GetServiceName(ctx, serviceName)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrGettingNetworkingServiceName, err)
	}
	return serviceName, nil
}

func (pm *pricingMap) getPricing(ctx context.Context) ([]*billingpb.Sku, error) {
	serviceName, err := pm.getServiceName(ctx)
	if err != nil {
		return nil, err
	}

	skus := pm.gcpClient.GetPricing(ctx, serviceName)
	if len(skus) == 0 {
		return nil, fmt.Errorf("%w: %w", ErrNoSKUsFoundForNetworkingService, err)
	}
	return skus, nil
}

func (pm *pricingMap) populate(ctx context.Context) error {
	pm.logger.LogAttrs(ctx, slog.LevelInfo, "Populating pricing map")
	skus, err := pm.getPricing(ctx)
	if err != nil {
		return err
	}
	skuData, err := pm.parseSku(skus)
	if err != nil {
		return err
	}
	if err := pm.processSkuData(skuData); err != nil {
		return err
	}

	return nil
}

func (pm *pricingMap) parseSku(skus []*billingpb.Sku) ([]*ParsedSkuData, error) {
	var skuData []*ParsedSkuData
	for _, sku := range skus {
		if sku.Category.ResourceGroup == ResourceGroup {
			if len(sku.GeoTaxonomy.Regions) == 0 || len(sku.PricingInfo) == 0 || len(sku.PricingInfo[0].PricingExpression.TieredRates) == 0 {
				continue
			}
			region := sku.GeoTaxonomy.Regions[0]
			price := float64(sku.PricingInfo[0].PricingExpression.TieredRates[0].UnitPrice.Nanos) / 1e9

			for _, desc := range []string{
				forwardingRuleDescription,
				outboundDataProcessedDescription,
				inboundDataProcessedDescription,
			} {
				if strings.Contains(sku.Description, desc) {
					skuData = append(skuData, NewParsedSkuData(region, price, desc))
				}
			}
		}
	}
	return skuData, nil
}

func (pm *pricingMap) processSkuData(skuData []*ParsedSkuData) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	for _, data := range skuData {
		if _, ok := pm.pricing[data.Region]; !ok {
			pm.pricing[data.Region] = NewPricing()
		}

		switch data.Description {
		case forwardingRuleDescription:
			pm.setForwardingRuleCost(data.Region, data.Price)
		case inboundDataProcessedDescription:
			pm.setInboundDataProcessedCost(data.Region, data.Price)
		case outboundDataProcessedDescription:
			pm.setOutboundDataProcessedCost(data.Region, data.Price)
		default:
			return fmt.Errorf("%w: %s", ErrUnknownDescription, data.Description)
		}

	}
	return nil
}

func (pm *pricingMap) setForwardingRuleCost(region string, price float64) {
	pm.pricing[region].forwardingRuleCost = price
}

func (pm *pricingMap) setInboundDataProcessedCost(region string, price float64) {
	pm.pricing[region].inboundDataProcessedCost = price
}

func (pm *pricingMap) setOutboundDataProcessedCost(region string, price float64) {
	pm.pricing[region].outboundDataProcessedCost = price
}

func (pm *pricingMap) GetCostOfForwardingRule(region string) (float64, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	if _, ok := pm.pricing[region]; !ok {
		return 0, fmt.Errorf("%w: %s", ErrRegionNotFound, region)
	}
	return pm.pricing[region].forwardingRuleCost, nil
}

func (pm *pricingMap) GetCostOfInboundData(region string) (float64, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	if _, ok := pm.pricing[region]; !ok {
		return 0, fmt.Errorf("%w: %s", ErrRegionNotFound, region)
	}
	return pm.pricing[region].inboundDataProcessedCost, nil
}

func (pm *pricingMap) GetCostOfOutboundData(region string) (float64, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	if _, ok := pm.pricing[region]; !ok {
		return 0, fmt.Errorf("%w: %s", ErrRegionNotFound, region)
	}
	return pm.pricing[region].outboundDataProcessedCost, nil
}
