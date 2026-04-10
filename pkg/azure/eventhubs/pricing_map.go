package eventhubs

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/grafana/cloudcost-exporter/pkg/azure/client"
	retailPriceSdk "gomodules.xyz/azure-retail-prices-sdk-for-go/sdk"
)

const (
	priceRefreshTTL  = 24 * time.Hour
	globalRegionName = "global"

	throughputUnitComponent = "throughput_unit"
	kafkaEndpointComponent  = "kafka_endpoint"
	ingressComponent        = "ingress"
	blobStorageComponent    = "blob_storage"
)

type priceSnapshot struct {
	byRegion map[string]map[string]float64
}

// Snapshot is an immutable view of the current Event Hubs pricing data.
type Snapshot struct {
	ptr *priceSnapshot
}

// Price returns the unit price for one component in one region.
// Event Hubs components fall back to the global retail API rows when a
// region-specific entry is missing. Blob Storage pricing remains region-specific.
func (s Snapshot) Price(region, component string) (float64, bool) {
	if s.ptr == nil {
		return 0, false
	}

	region = normalizeKey(region)
	component = strings.ToLower(strings.TrimSpace(component))
	if region == "" || component == "" {
		return 0, false
	}

	if prices, ok := s.ptr.byRegion[region]; ok {
		if price, ok := prices[component]; ok && price > 0 {
			return price, true
		}
	}

	if component == blobStorageComponent {
		return 0, false
	}

	if globalPrices, ok := s.ptr.byRegion[globalRegionName]; ok {
		if price, ok := globalPrices[component]; ok && price > 0 {
			return price, true
		}
	}

	return 0, false
}

type pricingMap struct {
	logger      *slog.Logger
	azureClient client.AzureClient

	current atomic.Pointer[priceSnapshot]

	refreshMu   sync.Mutex
	nextRefresh time.Time
}

func newPricingMap(logger *slog.Logger, azureClient client.AzureClient) *pricingMap {
	pm := &pricingMap{
		logger:      logger.With("store", "pricing"),
		azureClient: azureClient,
	}
	pm.current.Store(&priceSnapshot{byRegion: make(map[string]map[string]float64)})
	return pm
}

func (pm *pricingMap) Snapshot() Snapshot {
	return Snapshot{ptr: pm.current.Load()}
}

func (pm *pricingMap) RefreshIfNeeded(ctx context.Context, regions []string) error {
	if len(regions) == 0 {
		return nil
	}

	pm.refreshMu.Lock()
	defer pm.refreshMu.Unlock()

	snapshot := pm.Snapshot()
	if time.Now().Before(pm.nextRefresh) && snapshot.hasAllRegions(regions) {
		return nil
	}

	next, err := pm.buildSnapshot(ctx, regions)
	if err != nil {
		return err
	}

	pm.current.Store(next)
	pm.nextRefresh = time.Now().Add(priceRefreshTTL)
	return nil
}

func (s Snapshot) hasAllRegions(regions []string) bool {
	requiredComponents := []string{
		throughputUnitComponent,
		kafkaEndpointComponent,
		ingressComponent,
		blobStorageComponent,
	}

	for _, region := range regions {
		for _, component := range requiredComponents {
			if _, ok := s.Price(region, component); !ok {
				return false
			}
		}
	}

	return true
}

func (pm *pricingMap) buildSnapshot(ctx context.Context, regions []string) (*priceSnapshot, error) {
	snapshot := &priceSnapshot{byRegion: make(map[string]map[string]float64)}

	eventHubsPricing, err := pm.fetchEventHubsPricing(ctx, regions)
	if err != nil {
		return nil, err
	}

	blobStoragePricing, err := pm.fetchBlobStoragePricing(ctx, regions)
	if err != nil {
		return nil, err
	}

	for region, components := range eventHubsPricing {
		for component, price := range components {
			snapshot.setPrice(region, component, price)
		}
	}

	for region, price := range blobStoragePricing {
		snapshot.setPrice(region, blobStorageComponent, price)
	}

	return snapshot, nil
}

func (ps *priceSnapshot) setPrice(region, component string, price float64) {
	if region == "" || component == "" || price == 0 {
		return
	}

	if ps.byRegion[region] == nil {
		ps.byRegion[region] = make(map[string]float64)
	}
	ps.byRegion[region][component] = price
}

func (pm *pricingMap) fetchEventHubsPricing(ctx context.Context, regions []string) (map[string]map[string]float64, error) {
	filter := fmt.Sprintf(
		"serviceName eq '%s' and priceType eq 'Consumption' and (%s)",
		eventHubsServiceName,
		armRegionFilter(regions, true),
	)

	prices, err := pm.azureClient.ListPrices(ctx, &retailPriceSdk.RetailPricesClientListOptions{
		APIVersion: to.Ptr(retailPriceAPIVersion),
		Filter:     to.Ptr(filter),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list Event Hubs retail prices: %w", err)
	}

	byRegion := make(map[string]map[string]float64)
	for _, price := range prices {
		component, region, unitPrice, ok := classifyEventHubsPrice(price)
		if !ok {
			continue
		}

		if byRegion[region] == nil {
			byRegion[region] = make(map[string]float64)
		}
		byRegion[region][component] = maxFloat(byRegion[region][component], unitPrice)
	}

	return byRegion, nil
}

func classifyEventHubsPrice(price *retailPriceSdk.ResourceSKU) (string, string, float64, bool) {
	if price == nil {
		return "", "", 0, false
	}

	region := normalizeKey(price.ArmRegionName)
	if region == "" {
		return "", "", 0, false
	}

	switch {
	case price.ProductName == eventHubsServiceName &&
		price.SkuName == standardSKUName &&
		price.MeterName == standardThroughputMeter &&
		strings.EqualFold(price.UnitOfMeasure, "1 Hour"):
		return throughputUnitComponent, region, price.RetailPrice, true
	case price.ProductName == eventHubsServiceName &&
		price.SkuName == standardSKUName &&
		price.MeterName == standardKafkaEndpointMeter &&
		strings.EqualFold(price.UnitOfMeasure, "1 Hour"):
		return kafkaEndpointComponent, region, price.RetailPrice, true
	case price.ProductName == eventHubsServiceName &&
		price.SkuName == standardSKUName &&
		price.MeterName == standardIngressMeter &&
		strings.EqualFold(price.UnitOfMeasure, "1M"):
		return ingressComponent, region, price.RetailPrice, true
	default:
		return "", "", 0, false
	}
}

func (pm *pricingMap) fetchBlobStoragePricing(ctx context.Context, regions []string) (map[string]float64, error) {
	filter := fmt.Sprintf(
		"serviceName eq '%s' and priceType eq 'Consumption' and skuName eq '%s' and meterName eq '%s' and (%s) and (productName eq '%s' or productName eq '%s' or productName eq '%s')",
		storageServiceName,
		hotLRSSKUName,
		hotLRSDataStoredMeter,
		armRegionFilter(regions, false),
		blobStorageProductName,
		generalBlockBlobProductV2,
		generalBlockBlobProduct,
	)

	prices, err := pm.azureClient.ListPrices(ctx, &retailPriceSdk.RetailPricesClientListOptions{
		APIVersion: to.Ptr(retailPriceAPIVersion),
		Filter:     to.Ptr(filter),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list Blob Storage retail prices: %w", err)
	}

	type candidate struct {
		productPreference int
		price             float64
	}

	selected := make(map[string]candidate)
	for _, price := range prices {
		if price == nil || !strings.EqualFold(price.UnitOfMeasure, "1 GB/Month") {
			continue
		}

		region := normalizeKey(price.ArmRegionName)
		if region == "" {
			continue
		}

		preference, ok := blobProductPreference(price.ProductName)
		if !ok {
			continue
		}

		current, exists := selected[region]
		if !exists || preference < current.productPreference || (preference == current.productPreference && price.RetailPrice > current.price) {
			selected[region] = candidate{
				productPreference: preference,
				price:             price.RetailPrice,
			}
		}
	}

	pricingByRegion := make(map[string]float64, len(selected))
	for region, candidate := range selected {
		pricingByRegion[region] = candidate.price
	}

	return pricingByRegion, nil
}
