// Package aks provides Azure Kubernetes Service (AKS) cost collection functionality.
// This file implements disk pricing store with chunked/background pricing population
// to prevent startup hangs while providing comprehensive pricing data.
//
// Auto-generation maintenance:
// The mapClusterRegionToPricingRegion function and disk SKU functions are auto-generated from the Azure Retail Prices API.
// To update the region mapping and disk SKU/tier functions, run: go generate ./pkg/azure/aks
// This will fetch current data from the Azure Retail Prices API and regenerate both mappings.

//go:generate go run -tags generate ../generate/generate_regions.go
//go:generate go run -tags generate ../generate/generate_disk_skus.go

package aks

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/Azure/go-autorest/autorest/to"
	"github.com/grafana/cloudcost-exporter/pkg/azure/client"
	retailPriceSdk "gomodules.xyz/azure-retail-prices-sdk-for-go/sdk"
)

const (
	// diskRefreshInterval defines how often to refresh disk inventory and pricing
	diskRefreshInterval = 15 * time.Minute
)

var (
	// ErrDiskPriceNotFound is returned when pricing cannot be found for a disk
	ErrDiskPriceNotFound = fmt.Errorf("disk price not found")
)

// DiskPricing represents pricing information for an Azure disk SKU in a specific location.
// Retrieved from Azure Retail Prices API and mapped to internal pricing keys.
type DiskPricing struct {
	SKU         string  // Azure pricing SKU name (e.g., "P15 LRS Disk")
	Location    string  // Azure pricing region name (e.g., "US Central")
	RetailPrice float64 // Monthly retail price in USD
	Unit        string  // Unit of measure (typically "1/Month")
}

// DiskStore manages Azure disk inventory and pricing data with background population.
// Implements chunked pricing strategy to prevent startup hangs while ensuring comprehensive coverage.
type DiskStore struct {
	ctx         context.Context         // Parent context for operations
	logger      *slog.Logger            // Logger with "store=disk" context
	azClient    client.AzureClient      // Azure client for API calls
	mu          sync.RWMutex            // Protects concurrent access to maps
	disks       map[string]*Disk        // Disk inventory keyed by disk name
	diskPricing map[string]*DiskPricing // Pricing data keyed by "sku-location"
	lastRefresh time.Time               // Last successful disk inventory refresh
}

// NewDiskStore creates a new DiskStore with immediate disk inventory population and background pricing.
// Disk inventory is populated synchronously (fast), while pricing is loaded in background to prevent startup hangs.
func NewDiskStore(ctx context.Context, logger *slog.Logger, azClient client.AzureClient) *DiskStore {
	ds := &DiskStore{
		ctx:         ctx,
		logger:      logger.With("store", "disk"),
		azClient:    azClient,
		disks:       make(map[string]*Disk),
		diskPricing: make(map[string]*DiskPricing),
	}

	// Populate disk inventory immediately (this is fast)
	ds.PopulateDiskStore(ctx)

	// Populate pricing in background (this can be slow)
	go func() {
		if err := ds.PopulateDiskPricing(ctx); err != nil {
			ds.logger.LogAttrs(ctx, slog.LevelError, "failed to populate disk pricing in background", slog.String("error", err.Error()))
		}
	}()

	return ds
}

// PopulateDiskStore refreshes the disk inventory from Azure subscription.
// This operation is fast (~6 seconds) and safe to run synchronously during startup.
func (ds *DiskStore) PopulateDiskStore(ctx context.Context) error {
	ds.logger.LogAttrs(ctx, slog.LevelInfo, "populating disk store")

	disks, err := ds.azClient.ListDisksInSubscription(ctx)
	if err != nil {
		ds.logger.LogAttrs(ctx, slog.LevelError, "failed to list disks", slog.String("error", err.Error()))
		return err
	}

	ds.mu.Lock()
	defer ds.mu.Unlock()

	ds.disks = make(map[string]*Disk)
	for _, disk := range disks {
		if disk.Name != nil {
			d := NewDisk(disk)
			if d != nil {
				ds.disks[*disk.Name] = d
			}
		}
	}

	ds.lastRefresh = time.Now()
	ds.logger.LogAttrs(ctx, slog.LevelInfo, "disk store populated", slog.Int("disk_count", len(ds.disks)))
	return nil
}

// PopulateDiskPricing loads disk pricing data from Azure Retail Prices API.
// Uses global pricing strategy with 5-minute timeout to handle API performance issues.
// This operation runs in background to prevent startup hangs.
func (ds *DiskStore) PopulateDiskPricing(ctx context.Context) error {
	ds.logger.LogAttrs(ctx, slog.LevelInfo, "populating disk pricing")

	// Use global pricing strategy for comprehensive coverage and performance.
	// Regional chunking was attempted but proved slower and less reliable.

	// Clear existing pricing data
	ds.mu.Lock()
	ds.diskPricing = make(map[string]*DiskPricing)
	ds.mu.Unlock()

	// Load global pricing data with shorter timeout
	if err := ds.loadGlobalPricing(ctx); err != nil {
		ds.logger.LogAttrs(ctx, slog.LevelError, "failed to load global disk pricing", slog.String("error", err.Error()))
		return err
	}

	ds.mu.RLock()
	pricingCount := len(ds.diskPricing)
	ds.mu.RUnlock()

	ds.logger.LogAttrs(ctx, slog.LevelInfo, "disk pricing populated",
		slog.Int("pricing_count", pricingCount))
	return nil
}

// loadGlobalPricing loads all Azure Managed Disk pricing data using a global filter.
// More efficient than regional chunking while providing comprehensive coverage.
// Uses 5-minute timeout to handle Azure Retail Prices API performance variations.
func (ds *DiskStore) loadGlobalPricing(ctx context.Context) error {
	// Use shorter timeout for global pricing (5 minutes)
	pricingCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	// Global filter for all managed disk pricing data
	filter := "serviceName eq 'Storage' and contains(productName, 'Managed Disk') and priceType eq 'Consumption'"

	opts := &retailPriceSdk.RetailPricesClientListOptions{
		APIVersion: to.StringPtr(AZ_API_VERSION),
		Filter:     to.StringPtr(filter),
	}

	ds.logger.LogAttrs(ctx, slog.LevelDebug, "loading global disk pricing",
		slog.String("filter", filter))

	prices, err := ds.azClient.ListPrices(pricingCtx, opts)
	if err != nil {
		return fmt.Errorf("failed to list global disk prices: %w", err)
	}

	ds.logger.LogAttrs(ctx, slog.LevelDebug, "received global pricing data",
		slog.Int("price_count", len(prices)))

	// Store pricing data
	ds.mu.Lock()
	defer ds.mu.Unlock()

	storedCount := 0
	for _, price := range prices {
		if price.MeterName != "" && price.Location != "" {
			key := ds.buildPricingKey(price.MeterName, price.Location)
			ds.diskPricing[key] = &DiskPricing{
				SKU:         price.MeterName,
				Location:    price.Location,
				RetailPrice: price.RetailPrice,
				Unit:        price.UnitOfMeasure,
			}
			storedCount++

			// Debug: log first few pricing entries to see the format
			if len(ds.diskPricing) <= 5 {
				ds.logger.LogAttrs(ctx, slog.LevelDebug, "disk pricing example",
					slog.String("meterName", price.MeterName),
					slog.String("location", price.Location),
					slog.String("key", key),
					slog.String("serviceName", price.ServiceName),
					slog.Float64("retailPrice", price.RetailPrice))
			}
		}
	}

	ds.logger.LogAttrs(ctx, slog.LevelDebug, "stored global pricing",
		slog.Int("stored_prices", storedCount))

	return nil
}

// GetDiskPricing retrieves pricing for a specific disk based on its SKU, and location.
func (ds *DiskStore) GetDiskPricing(disk *Disk) (*DiskPricing, error) {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	key := ds.buildDiskPricingKey(disk)
	if pricing, ok := ds.diskPricing[key]; ok {
		return pricing, nil
	}

	// Debug: show what we're looking for vs what we have
	ds.logger.LogAttrs(context.Background(), slog.LevelDebug, "disk pricing lookup failed",
		slog.String("diskName", disk.Name),
		slog.String("diskSKU", disk.SKU),
		slog.Int("diskSize", int(disk.Size)),
		slog.String("diskLocation", disk.Location),
		slog.String("requestedKey", key),
		slog.Int("availablePrices", len(ds.diskPricing)))

	return nil, fmt.Errorf("%w: key=%s", ErrDiskPriceNotFound, key)
}

// GetAllDisks returns a _copy_ of all disks in the store.
func (ds *DiskStore) GetAllDisks() map[string]*Disk {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	result := make(map[string]*Disk)
	for k, v := range ds.disks {
		result[k] = v
	}
	return result
}

// buildDiskPricingKey builds a _unique_ key for disk pricing lookup from SKU and location.
func (ds *DiskStore) buildDiskPricingKey(disk *Disk) string {
	skuForPricing := ds.mapDiskSKUToPricingSKU(disk.SKU, disk.Size)
	pricingRegion := ds.mapClusterRegionToPricingRegion(disk.Location)
	return ds.buildPricingKey(skuForPricing, pricingRegion)
}

// mapClusterRegionToPricingRegion is implemented in region_mapping_generated.go
// This function is auto-generated from the Azure Retail Prices API.
// To regenerate: go generate ./pkg/azure/aks

func (ds *DiskStore) buildPricingKey(sku, location string) string {
	return fmt.Sprintf("%s-%s", strings.ToLower(sku), strings.ToLower(location))
}

// mapDiskSKUToPricingSKU maps Azure disk SKUs to Azure Retail Prices API SKU names.
// Azure uses fixed pricing tiers based on disk size, not actual size requested.
// Example: 100GB Premium_LRS disk maps to "P15 LRS Disk" (256GB tier) pricing.
func (ds *DiskStore) mapDiskSKUToPricingSKU(diskSKU string, sizeGB int32) string {
	switch diskSKU {
	case "Standard_LRS":
		return ds.getStandardHDDSKU(sizeGB)
	case "StandardSSD_LRS":
		return ds.getStandardSSDSKU(sizeGB)
	case "Premium_LRS":
		return ds.getPremiumSSDSKU(sizeGB)
	case "PremiumV2_LRS":
		return "Premium SSD v2"
	case "UltraSSD_LRS":
		return "Ultra Disk"
	default:
		return diskSKU
	}
}

// Disk SKU functions are implemented in disk_skus_generated.go
// These functions (getStandardHDDSKU, getStandardSSDSKU, getPremiumSSDSKU, extractTierFromSKU)
// are auto-generated from the Azure Retail Prices API.
// To regenerate: make generate
