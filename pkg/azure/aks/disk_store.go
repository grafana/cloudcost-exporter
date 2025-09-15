// Package aks provides Azure Kubernetes Service (AKS) cost collection functionality.
// This file implements disk pricing store with chunked/background pricing population
// to prevent startup hangs while providing comprehensive pricing data.
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
	ctx         context.Context                // Parent context for operations
	logger      *slog.Logger                   // Logger with "store=disk" context
	azClient    client.AzureClient             // Azure client for API calls
	mu          sync.RWMutex                   // Protects concurrent access to maps
	disks       map[string]*Disk               // Disk inventory keyed by disk name
	diskPricing map[string]*DiskPricing        // Pricing data keyed by "sku-location"
	lastRefresh time.Time                      // Last successful disk inventory refresh
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

func (ds *DiskStore) getUniqueRegionsFromDisks() []string {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	regionSet := make(map[string]bool)
	for _, disk := range ds.disks {
		if disk.Location != "" {
			regionSet[disk.Location] = true
		}
	}

	regions := make([]string, 0, len(regionSet))
	for region := range regionSet {
		regions = append(regions, region)
	}
	return regions
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

func (ds *DiskStore) GetAllDisks() map[string]*Disk {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	result := make(map[string]*Disk)
	for k, v := range ds.disks {
		result[k] = v
	}
	return result
}

func (ds *DiskStore) GetKubernetesDisks() map[string]*Disk {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	result := make(map[string]*Disk)
	for k, v := range ds.disks {
		if v.IsKubernetesPV() {
			result[k] = v
		}
	}
	return result
}

func (ds *DiskStore) buildDiskPricingKey(disk *Disk) string {
	skuForPricing := ds.mapDiskSKUToPricingSKU(disk.SKU, disk.Size)
	pricingRegion := ds.mapClusterRegionToPricingRegion(disk.Location)
	return ds.buildPricingKey(skuForPricing, pricingRegion)
}

// mapClusterRegionToPricingRegion maps Azure cluster region names to Azure Retail Prices API region names.
// The pricing API uses different region naming conventions than ARM resources.
// Example: "centralus" (ARM) -> "US Central" (Pricing API)
func (ds *DiskStore) mapClusterRegionToPricingRegion(clusterRegion string) string {
	// Comprehensive mapping based on observed Azure Retail Prices API region names
	regionMap := map[string]string{
		"centralus":      "US Central",
		"eastus":         "US East",
		"eastus2":        "US East 2",
		"westus":         "US West",
		"westus2":        "US West 2",
		"westus3":        "US West 3",
		"northcentralus": "US North Central",
		"southcentralus": "US South Central",
		"westcentralus":  "US West Central",

		// European regions - corrected based on Azure Retail Prices API format
		"westeurope":         "EU West",
		"northeurope":        "EU North",
		"uksouth":            "UK South",
		"ukwest":             "UK West",
		"francecentral":      "FR Central",
		"francesouth":        "FR South",
		"germanywestcentral": "DE West Central",
		"germanynorth":       "DE North",
		"norwayeast":         "NO East",
		"norwaywest":         "NO West",
		"switzerlandnorth":   "CH North",
		"switzerlandwest":    "CH West",

		// Asian regions - based on observed API format
		"eastasia":           "AP East",
		"southeastasia":      "AP Southeast",
		"japaneast":          "JA East",
		"japanwest":          "JA West",
		"australiaeast":      "AU East",
		"australiasoutheast": "AU Southeast",
		"australiacentral":   "AU Central",
		"australiacentral2":  "AU Central 2",
		"koreacentral":       "KR Central",
		"koreasouth":         "KR South",
		"southindia":         "IN South",
		"centralindia":       "IN Central",
		"westindia":          "IN West",

		// Additional regions based on observed API patterns
		"canadacentral":    "CA Central",
		"canadaeast":       "CA East",
		"brazilsouth":      "BR South",
		"brazilsoutheast":  "BR Southeast",
		"southafricanorth": "ZA North",
		"southafricawest":  "ZA West",
		"uaenorth":         "AE North",
		"uaecentral":       "AE Central",
	}

	if pricingRegion, ok := regionMap[clusterRegion]; ok {
		return pricingRegion
	}

	// If no mapping found, return original (might work)
	return clusterRegion
}

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

// getStandardHDDSKU maps disk size to Standard HDD pricing tier.
// Azure Standard HDD pricing tiers: S4(32GB), S6(64GB), S10(128GB), etc.
func (ds *DiskStore) getStandardHDDSKU(sizeGB int32) string {
	if sizeGB <= 32 {
		return "S4 LRS Disk"
	} else if sizeGB <= 64 {
		return "S6 LRS Disk"
	} else if sizeGB <= 128 {
		return "S10 LRS Disk"
	} else if sizeGB <= 256 {
		return "S15 LRS Disk"
	} else if sizeGB <= 512 {
		return "S20 LRS Disk"
	} else if sizeGB <= 1024 {
		return "S30 LRS Disk"
	} else if sizeGB <= 2048 {
		return "S40 LRS Disk"
	} else if sizeGB <= 4096 {
		return "S50 LRS Disk"
	} else if sizeGB <= 8192 {
		return "S60 LRS Disk"
	} else if sizeGB <= 16384 {
		return "S70 LRS Disk"
	} else {
		return "S80 LRS Disk"
	}
}

// getStandardSSDSKU maps disk size to Standard SSD pricing tier.
// Azure Standard SSD pricing tiers: E1(4GB), E2(8GB), E3(16GB), E4(32GB), etc.
func (ds *DiskStore) getStandardSSDSKU(sizeGB int32) string {
	if sizeGB <= 4 {
		return "E1 LRS Disk"
	} else if sizeGB <= 8 {
		return "E2 LRS Disk"
	} else if sizeGB <= 16 {
		return "E3 LRS Disk"
	} else if sizeGB <= 32 {
		return "E4 LRS Disk"
	} else if sizeGB <= 64 {
		return "E6 LRS Disk"
	} else if sizeGB <= 128 {
		return "E10 LRS Disk"
	} else if sizeGB <= 256 {
		return "E15 LRS Disk"
	} else if sizeGB <= 512 {
		return "E20 LRS Disk"
	} else if sizeGB <= 1024 {
		return "E30 LRS Disk"
	} else if sizeGB <= 2048 {
		return "E40 LRS Disk"
	} else if sizeGB <= 4096 {
		return "E50 LRS Disk"
	} else if sizeGB <= 8192 {
		return "E60 LRS Disk"
	} else if sizeGB <= 16384 {
		return "E70 LRS Disk"
	} else {
		return "E80 LRS Disk"
	}
}

// getPremiumSSDSKU maps disk size to Premium SSD pricing tier.
// Azure Premium SSD pricing tiers: P4(32GB), P6(64GB), P10(128GB), P15(256GB), etc.
func (ds *DiskStore) getPremiumSSDSKU(sizeGB int32) string {
	if sizeGB <= 32 {
		return "P4 LRS Disk"
	} else if sizeGB <= 64 {
		return "P6 LRS Disk"
	} else if sizeGB <= 128 {
		return "P10 LRS Disk"
	} else if sizeGB <= 256 {
		return "P15 LRS Disk"
	} else if sizeGB <= 512 {
		return "P20 LRS Disk"
	} else if sizeGB <= 1024 {
		return "P30 LRS Disk"
	} else if sizeGB <= 2048 {
		return "P40 LRS Disk"
	} else if sizeGB <= 4096 {
		return "P50 LRS Disk"
	} else if sizeGB <= 8192 {
		return "P60 LRS Disk"
	} else if sizeGB <= 16384 {
		return "P70 LRS Disk"
	} else {
		return "P80 LRS Disk"
	}
}
