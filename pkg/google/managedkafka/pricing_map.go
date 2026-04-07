package managedkafka

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"cloud.google.com/go/billing/apiv1/billingpb"
	"github.com/grafana/cloudcost-exporter/pkg/google/client"
	"github.com/grafana/cloudcost-exporter/pkg/utils"
)

const (
	managedKafkaServiceName = "Managed Service for Apache Kafka"

	computeComponent      = "compute"
	localStorageComponent = "local_storage"

	legacyComputeDescription    = "cpu+ram"
	currentComputeDescription   = "data compute units"
	connectDescription          = "connect"
	localStorageDescription     = "local storage"
	longTermStorageDescription  = "long term"
	storageDescription          = "storage"
	monthlyUsageUnitDescription = "month"
	monthlyUsageUnitSuffix      = ".mo"
)

type pricing struct {
	compute      *priceEntry
	localStorage *priceEntry
}

type priceEntry struct {
	value  float64
	source string
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

		price, ok := priceForSKU(sku, component)
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

			if err := pricingByRegion[region].setPrice(component, sku.GetDescription(), price, region); err != nil {
				return err
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

func (p *pricing) setPrice(component, description string, price float64, region string) error {
	switch component {
	case computeComponent:
		return setPriceEntry(&p.compute, "compute", description, price, region)
	case localStorageComponent:
		return setPriceEntry(&p.localStorage, "local storage", description, price, region)
	}

	return nil
}

func setPriceEntry(current **priceEntry, component, description string, price float64, region string) error {
	if *current != nil {
		if (*current).value != price {
			return fmt.Errorf(
				"multiple %s prices found for region %s: %q=%v, %q=%v",
				component,
				region,
				(*current).source,
				(*current).value,
				description,
				price,
			)
		}
		return nil
	}

	*current = &priceEntry{
		value:  price,
		source: description,
	}

	return nil
}

func (pm *pricingMap) ComputePricePerDCUHour(region string) (float64, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	pricing, ok := pm.pricing[region]
	if !ok || pricing.compute == nil || pricing.compute.value == 0 {
		return 0, fmt.Errorf("compute pricing not found for region %s", region)
	}

	return pricing.compute.value, nil
}

func (pm *pricingMap) LocalStoragePricePerGiBHour(region string) (float64, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	pricing, ok := pm.pricing[region]
	if !ok || pricing.localStorage == nil || pricing.localStorage.value == 0 {
		return 0, fmt.Errorf("local storage pricing not found for region %s", region)
	}

	return pricing.localStorage.value, nil
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

	description = strings.ToLower(description)

	switch {
	case strings.Contains(description, connectDescription) &&
		(strings.Contains(description, legacyComputeDescription) || strings.Contains(description, currentComputeDescription)):
		return "", false
	case strings.Contains(description, legacyComputeDescription), strings.Contains(description, currentComputeDescription):
		return computeComponent, true
	case strings.Contains(description, longTermStorageDescription) && strings.Contains(description, storageDescription):
		return "", false
	case strings.Contains(description, localStorageDescription):
		return localStorageComponent, true
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

func priceForSKU(sku *billingpb.Sku, component string) (float64, bool) {
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

	price := float64(rate.GetUnits()) + float64(rate.GetNanos())/1e9
	if component == localStorageComponent && isMonthlyUsage(expression) {
		return price / utils.HoursInMonth, true
	}

	return price, true
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

func isMonthlyUsage(expression *billingpb.PricingExpression) bool {
	if expression == nil {
		return false
	}

	usageUnitDescription := strings.ToLower(expression.GetUsageUnitDescription())
	if strings.Contains(usageUnitDescription, monthlyUsageUnitDescription) {
		return true
	}

	usageUnit := strings.ToLower(expression.GetUsageUnit())
	return strings.Contains(usageUnit, monthlyUsageUnitSuffix)
}
