package managedkafka

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
	managedKafkaServiceName = "Managed Service for Apache Kafka"

	computeDescription         = "CPU+RAM"
	connectComputeDescription  = "Connect CPU+RAM"
	localStorageDescription    = "Local Storage"
	longTermStorageDescription = "Long term storage"
)

type pricing struct {
	computePricePerDCUHour      float64
	localStoragePricePerGiBHour float64
}

type pricingMap struct {
	logger    *slog.Logger
	gcpClient client.Client
	mu        sync.RWMutex
	pricing   map[string]*pricing
}

func newPricingMap(ctx context.Context, logger *slog.Logger, gcpClient client.Client) (*pricingMap, error) {
	pm := &pricingMap{
		logger:    logger,
		gcpClient: gcpClient,
		pricing:   make(map[string]*pricing),
	}

	if err := pm.populate(ctx); err != nil {
		return nil, err
	}

	return pm, nil
}

func (pm *pricingMap) populate(ctx context.Context) error {
	serviceName, err := pm.gcpClient.GetServiceName(ctx, managedKafkaServiceName)
	if err != nil {
		return fmt.Errorf("failed to get service name for Managed Kafka: %w", err)
	}

	skus := pm.gcpClient.GetPricing(ctx, serviceName)
	if len(skus) == 0 {
		return fmt.Errorf("no SKUs found for Managed Kafka service")
	}

	pricingByRegion := make(map[string]*pricing)
	for _, sku := range skus {
		component, ok := classifySKU(sku)
		if !ok {
			continue
		}

		price, ok := priceForSKU(sku)
		if !ok {
			continue
		}

		for _, region := range skuRegions(sku) {
			if region == "" {
				continue
			}

			if _, exists := pricingByRegion[region]; !exists {
				pricingByRegion[region] = &pricing{}
			}

			switch component {
			case computeDescription:
				if price > pricingByRegion[region].computePricePerDCUHour {
					pricingByRegion[region].computePricePerDCUHour = price
				}
			case localStorageDescription:
				if price > pricingByRegion[region].localStoragePricePerGiBHour {
					pricingByRegion[region].localStoragePricePerGiBHour = price
				}
			}
		}
	}

	if len(pricingByRegion) == 0 {
		return fmt.Errorf("no Managed Kafka pricing entries were parsed")
	}

	pm.mu.Lock()
	pm.pricing = pricingByRegion
	pm.mu.Unlock()

	return nil
}

func (pm *pricingMap) ComputePricePerDCUHour(region string) (float64, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	pricing, ok := pm.pricing[region]
	if !ok || pricing.computePricePerDCUHour == 0 {
		return 0, fmt.Errorf("compute pricing not found for region %s", region)
	}

	return pricing.computePricePerDCUHour, nil
}

func (pm *pricingMap) LocalStoragePricePerGiBHour(region string) (float64, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	pricing, ok := pm.pricing[region]
	if !ok || pricing.localStoragePricePerGiBHour == 0 {
		return 0, fmt.Errorf("local storage pricing not found for region %s", region)
	}

	return pricing.localStoragePricePerGiBHour, nil
}

func classifySKU(sku *billingpb.Sku) (string, bool) {
	if sku == nil {
		return "", false
	}

	description := sku.GetDescription()
	if description == "" {
		return "", false
	}

	if isDiscountedSKU(sku) {
		return "", false
	}

	switch {
	case strings.Contains(description, connectComputeDescription):
		return "", false
	case strings.Contains(description, computeDescription):
		return computeDescription, true
	case strings.Contains(description, longTermStorageDescription):
		return "", false
	case strings.Contains(description, localStorageDescription):
		return localStorageDescription, true
	default:
		return "", false
	}
}

func isDiscountedSKU(sku *billingpb.Sku) bool {
	description := strings.ToLower(sku.GetDescription())
	if strings.Contains(description, "cud") || strings.Contains(description, "commit") {
		return true
	}

	for _, pricingInfo := range sku.GetPricingInfo() {
		summary := strings.ToLower(pricingInfo.GetSummary())
		if strings.Contains(summary, "cud") || strings.Contains(summary, "commit") {
			return true
		}
	}

	return false
}

func priceForSKU(sku *billingpb.Sku) (float64, bool) {
	if sku == nil || len(sku.GetPricingInfo()) == 0 {
		return 0, false
	}

	expression := sku.GetPricingInfo()[0].GetPricingExpression()
	if expression == nil || len(expression.GetTieredRates()) == 0 {
		return 0, false
	}

	rate := expression.GetTieredRates()[len(expression.GetTieredRates())-1].GetUnitPrice()
	if rate == nil {
		return 0, false
	}

	return float64(rate.GetUnits()) + float64(rate.GetNanos())/1e9, true
}

func skuRegions(sku *billingpb.Sku) []string {
	if sku == nil {
		return nil
	}
	if len(sku.GetServiceRegions()) > 0 {
		return sku.GetServiceRegions()
	}
	if sku.GetGeoTaxonomy() != nil {
		return sku.GetGeoTaxonomy().GetRegions()
	}
	return nil
}
