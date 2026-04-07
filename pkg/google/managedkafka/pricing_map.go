package managedkafka

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"

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

type priceSnapshot struct {
	byRegion map[string]map[string]float64
}

// Snapshot is an immutable view of the pricing data published by the pricing map.
type Snapshot struct {
	ptr *priceSnapshot
}

// Price returns the hourly price for a component in a region.
func (s Snapshot) Price(region, component string) (float64, bool) {
	if s.ptr == nil {
		return 0, false
	}

	prices, ok := s.ptr.byRegion[region]
	if !ok {
		return 0, false
	}

	price, ok := prices[component]
	if !ok || price == 0 {
		return 0, false
	}

	return price, true
}

type pricingMap struct {
	logger    *slog.Logger
	gcpClient client.Client
	current   atomic.Pointer[priceSnapshot]
}

func (pm *pricingMap) Snapshot() Snapshot {
	return Snapshot{ptr: pm.current.Load()}
}

func newPricingMap(ctx context.Context, logger *slog.Logger, gcpClient client.Client) (*pricingMap, error) {
	pm := &pricingMap{
		logger:    logger,
		gcpClient: gcpClient,
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

	snapshot := &priceSnapshot{byRegion: make(map[string]map[string]float64)}
	sources := make(map[string]map[string]string) // region → component → description (conflict detection only)

	for _, sku := range skus {
		component, ok := classifySKU(sku)
		if !ok {
			continue
		}

		price, ok := priceForSKU(sku, component)
		if !ok {
			continue
		}

		description := sku.GetDescription()

		for _, region := range skuRegions(sku) {
			if region == "" {
				continue
			}

			if snapshot.byRegion[region] == nil {
				snapshot.byRegion[region] = make(map[string]float64)
				sources[region] = make(map[string]string)
			}

			if existingSource, exists := sources[region][component]; exists {
				if snapshot.byRegion[region][component] != price {
					return fmt.Errorf(
						"multiple %s prices found for region %s: %q=%v, %q=%v",
						component, region,
						existingSource, snapshot.byRegion[region][component],
						description, price,
					)
				}
				continue
			}

			snapshot.byRegion[region][component] = price
			sources[region][component] = description
		}
	}

	if len(snapshot.byRegion) == 0 {
		return fmt.Errorf("no Managed Kafka pricing entries were parsed")
	}

	pm.current.Store(snapshot)
	return nil
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
