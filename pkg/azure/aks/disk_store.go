package aks

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/grafana/cloudcost-exporter/pkg/azure/client"
	retailPriceSdk "gomodules.xyz/azure-retail-prices-sdk-for-go/sdk"
	"github.com/Azure/go-autorest/autorest/to"
)

const (
	diskRefreshInterval = 15 * time.Minute
)

var (
	ErrDiskPriceNotFound = fmt.Errorf("disk price not found")
)

type DiskPricing struct {
	SKU         string
	Location    string
	RetailPrice float64
	Unit        string
}

type DiskStore struct {
	ctx           context.Context
	logger        *slog.Logger
	azClient      client.AzureClient
	mu            sync.RWMutex
	disks         map[string]*Disk
	diskPricing   map[string]*DiskPricing
	lastRefresh   time.Time
}

func NewDiskStore(ctx context.Context, logger *slog.Logger, azClient client.AzureClient) *DiskStore {
	ds := &DiskStore{
		ctx:         ctx,
		logger:      logger.With("store", "disk"),
		azClient:    azClient,
		disks:       make(map[string]*Disk),
		diskPricing: make(map[string]*DiskPricing),
	}

	ds.PopulateDiskStore(ctx)
	ds.PopulateDiskPricing(ctx)

	return ds
}

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

func (ds *DiskStore) PopulateDiskPricing(ctx context.Context) error {
	ds.logger.LogAttrs(ctx, slog.LevelInfo, "populating disk pricing")

	filter := "serviceName eq 'Storage' and priceType eq 'Consumption'"
	opts := &retailPriceSdk.RetailPricesClientListOptions{
		APIVersion: to.StringPtr(AZ_API_VERSION),
		Filter:     to.StringPtr(filter),
	}

	prices, err := ds.azClient.ListPrices(ctx, opts)
	if err != nil {
		ds.logger.LogAttrs(ctx, slog.LevelError, "failed to list disk prices", slog.String("error", err.Error()))
		return err
	}

	ds.mu.Lock()
	defer ds.mu.Unlock()

	for _, price := range prices {
		if price.MeterName != "" && price.Location != "" {
			key := ds.buildPricingKey(price.MeterName, price.Location)
			ds.diskPricing[key] = &DiskPricing{
				SKU:         price.MeterName,
				Location:    price.Location,
				RetailPrice: price.RetailPrice,
				Unit:        price.UnitOfMeasure,
			}
			
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

	ds.logger.LogAttrs(ctx, slog.LevelInfo, "disk pricing populated", slog.Int("pricing_count", len(ds.diskPricing)))
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

func (ds *DiskStore) mapClusterRegionToPricingRegion(clusterRegion string) string {
	// Map cluster region names to pricing API region names
	regionMap := map[string]string{
		"centralus":   "US Central",
		"eastus":      "US East",
		"eastus2":     "US East 2", 
		"westus":      "US West",
		"westus2":     "US West 2",
		"westus3":     "US West 3",
		"northcentralus": "US North Central",
		"southcentralus": "US South Central",
		"westcentralus":  "US West Central",
		
		"westeurope":     "West Europe",
		"northeurope":    "North Europe",
		"uksouth":        "UK South",
		"ukwest":         "UK West",
		"francecentral":  "France Central",
		"francesouth":    "France South",
		"germanywestcentral": "Germany West Central",
		"norwayeast":     "Norway East",
		"switzerlandnorth": "Switzerland North",
		
		"eastasia":       "East Asia",
		"southeastasia":  "Southeast Asia",
		"japaneast":      "Japan East",
		"japanwest":      "Japan West",
		"australiaeast":  "Australia East",
		"australiasoutheast": "Australia Southeast",
		"koreacentral":   "Korea Central",
		"koreasouth":     "Korea South",
		"southindia":     "South India",
		"centralindia":   "Central India",
		"westindia":      "West India",
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

