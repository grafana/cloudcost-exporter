package aks

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute"
	"github.com/Azure/go-autorest/autorest/to"
	"github.com/grafana/cloudcost-exporter/pkg/azure/azureClientWrapper"
	retailPriceSdk "gomodules.xyz/azure-retail-prices-sdk-for-go/sdk"
	"gopkg.in/matryer/try.v1"
)

const (
	AzureVolumePriceSearchFilter = `serviceName eq 'Storage' and type eq 'Consumption'`
	AzureMeterRegion             = `'primary'`
	// TODO: These are shared with the instance price store, we should move them to a shared location
	listPricesMaxRetries = 5
	priceRefreshInterval = 24 * time.Hour
	monthlyUnitOfMeasure = "1 GB/Month"
)

type VolumeSku struct {
	RetailPrice float64
	SkuName     string
	Tier        string
	ArmSkuName  string
}

type VolumePriceBySkuName map[string]*VolumeSku
type VolumePriceByProductName map[string]VolumePriceBySkuName
type VolumePriceByRegion map[string]VolumePriceByProductName

type VolumePriceStore struct {
	logger  *slog.Logger
	context context.Context

	volumePriceByRegionLock *sync.RWMutex
	VolumePriceByRegion     VolumePriceByRegion

	azureClient azureClientWrapper.AzureClient
}

// diskSkuNameMapping maps Azure Compute API disk SKU names to pricing API SKU names
var diskSkuNameMapping = map[string]string{
	// Premium SSD (Premium_LRS)
	"Premium_LRS": "P10", // Default mapping, will be refined by size

	// Standard SSD (StandardSSD_LRS)
	"StandardSSD_LRS": "E10", // Default mapping, will be refined by size

	// Standard HDD (Standard_LRS)
	"Standard_LRS": "S10", // Default mapping, will be refined by size

	// Ultra Disk (UltraSSD_LRS)
	"UltraSSD_LRS": "Ultra",

	// Premium SSD v2 (Premium_ZRS)
	"Premium_ZRS": "P10", // Default mapping, will be refined by size

	// Standard SSD ZRS (StandardSSD_ZRS)
	"StandardSSD_ZRS": "E10", // Default mapping, will be refined by size
}

// getDiskSkuPricingName returns the pricing API SKU name based on the Compute API SKU name and disk size
func getDiskSkuPricingName(computeSkuName string, diskSizeGB int) string {
	baseSku, exists := diskSkuNameMapping[computeSkuName]
	if !exists {
		return computeSkuName // Return original if no mapping exists
	}

	// For certain disk types, we need to determine the tier based on size
	switch computeSkuName {
	case "Premium_LRS", "Premium_ZRS":
		return getPremiumSSDTier(diskSizeGB)
	case "StandardSSD_LRS", "StandardSSD_ZRS":
		return getStandardSSDTier(diskSizeGB)
	case "Standard_LRS":
		return getStandardHDDTier(diskSizeGB)
	default:
		return baseSku
	}
}

// getPremiumSSDTier returns the appropriate Premium SSD tier based on disk size
func getPremiumSSDTier(diskSizeGB int) string {
	switch {
	case diskSizeGB <= 4:
		return "P1"
	case diskSizeGB <= 8:
		return "P2"
	case diskSizeGB <= 16:
		return "P3"
	case diskSizeGB <= 32:
		return "P4"
	case diskSizeGB <= 64:
		return "P6"
	case diskSizeGB <= 128:
		return "P10"
	case diskSizeGB <= 256:
		return "P15"
	case diskSizeGB <= 512:
		return "P20"
	case diskSizeGB <= 1024:
		return "P30"
	case diskSizeGB <= 2048:
		return "P40"
	case diskSizeGB <= 4096:
		return "P50"
	case diskSizeGB <= 8192:
		return "P60"
	case diskSizeGB <= 16384:
		return "P70"
	// According to Azure docs: https://learn.microsoft.com/en-us/azure/virtual-machines/disks-types#premium-ssd-disk-sizes
	// P80 is for disks 16 TiB to 32 TiB (32,767 GiB)
	case diskSizeGB <= 32767:
		return "P80"
	default:
		return "P80" // Maximum tier
	}
}

// getStandardSSDTier returns the appropriate Standard SSD tier based on disk size
func getStandardSSDTier(diskSizeGB int) string {
	switch {
	case diskSizeGB <= 4:
		return "E1"
	case diskSizeGB <= 8:
		return "E2"
	case diskSizeGB <= 16:
		return "E3"
	case diskSizeGB <= 32:
		return "E4"
	case diskSizeGB <= 64:
		return "E6"
	case diskSizeGB <= 128:
		return "E10"
	case diskSizeGB <= 256:
		return "E15"
	case diskSizeGB <= 512:
		return "E20"
	case diskSizeGB <= 1024:
		return "E30"
	case diskSizeGB <= 2048:
		return "E40"
	case diskSizeGB <= 4096:
		return "E50"
	case diskSizeGB <= 8192:
		return "E60"
	case diskSizeGB <= 16384:
		return "E70"
	case diskSizeGB <= 32767:
		return "E80"
	default:
		return "E80" // Maximum tier
	}
}

// getStandardHDDTier returns the appropriate Standard HDD tier based on disk size
func getStandardHDDTier(diskSizeGB int) string {
	switch {
	case diskSizeGB <= 4:
		return "S1"
	case diskSizeGB <= 8:
		return "S2"
	case diskSizeGB <= 16:
		return "S3"
	case diskSizeGB <= 32:
		return "S4"
	case diskSizeGB <= 64:
		return "S6"
	case diskSizeGB <= 128:
		return "S10"
	case diskSizeGB <= 256:
		return "S15"
	case diskSizeGB <= 512:
		return "S20"
	case diskSizeGB <= 1024:
		return "S30"
	case diskSizeGB <= 2048:
		return "S40"
	case diskSizeGB <= 4096:
		return "S50"
	case diskSizeGB <= 8192:
		return "S60"
	case diskSizeGB <= 16384:
		return "S70"
	case diskSizeGB <= 32767:
		return "S80"
	default:
		return "S80" // Maximum tier
	}
}

func NewVolumePriceStore(ctx context.Context, logger *slog.Logger, azClient azureClientWrapper.AzureClient) *VolumePriceStore {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(nil, nil))
	}

	storeLogger := logger.With("subsystem", "volumePriceStore")

	p := &VolumePriceStore{
		logger:                  storeLogger,
		context:                 ctx,
		azureClient:             azClient,
		volumePriceByRegionLock: &sync.RWMutex{},
		VolumePriceByRegion:     make(VolumePriceByRegion),
	}

	// Populate the store before it is used
	go p.PopulatePriceStore(ctx)

	// Start a goroutine to periodically refresh prices
	go func() {
		ticker := time.NewTicker(priceRefreshInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				p.PopulatePriceStore(ctx)
			case <-ctx.Done():
				return
			}
		}
	}()

	return p
}

func (p *VolumePriceStore) GetVolumePrice(region string, skuName string) (*VolumeSku, error) {
	p.volumePriceByRegionLock.RLock()
	defer p.volumePriceByRegionLock.RUnlock()

	if len(region) == 0 || len(skuName) == 0 {
		p.logger.LogAttrs(p.context, slog.LevelError, "region or sku not defined",
			slog.String("region", region),
			slog.String("sku", skuName))
		return nil, ErrPriceInformationNotFound
	}

	regionMap := p.VolumePriceByRegion[region]
	if regionMap == nil {
		p.logger.LogAttrs(p.context, slog.LevelError, "region not found in price map",
			slog.String("region", region))
		return nil, ErrPriceInformationNotFound
	}

	volumeSku := regionMap[skuName]
	if volumeSku == nil {
		p.logger.LogAttrs(p.context, slog.LevelError, "sku info not found in region map",
			slog.String("sku", skuName),
			slog.String("region", region))
		return nil, ErrPriceInformationNotFound
	}

	return nil, nil
}

// validateVolumePriceIsRelevantFromSku checks if the price is relevant for persistent volumes
// It checks if the product name contains "Disk" and if the unit of measure is per month
// What are some relevant skues?
func validateVolumePriceIsRelevantFromSku(sku *retailPriceSdk.ResourceSKU) bool {
	if sku == nil {
		return false
	}

	// Storage serviceFamily returns many product names that we're not concerned about.
	// This filters out product names that are not associated with disks
	if !strings.Contains(sku.ProductName, "Disk") && !strings.Contains(sku.ProductName, "SSD") {
		return false
	}

	return true
}

// PopulatePriceStore populates the price store with the latest prices
// It fetches the prices from the Azure API and stores them in the price store
// It also logs the prices to the console
func (p *VolumePriceStore) PopulatePriceStore(ctx context.Context) {

	startTime := time.Now()

	p.logger.Info("populating volume price store")

	opts := &retailPriceSdk.RetailPricesClientListOptions{
		APIVersion:  to.StringPtr(AZ_API_VERSION),
		Filter:      to.StringPtr(AzureVolumePriceSearchFilter),
		MeterRegion: to.StringPtr(AzureMeterRegion),
	}

	var allPrices []*retailPriceSdk.ResourceSKU

	err := try.Do(func(attempt int) (bool, error) {
		var err error
		var prices []*retailPriceSdk.ResourceSKU

		prices, err = p.azureClient.ListPrices(ctx, opts)
		if err != nil {
			if attempt == listPricesMaxRetries {
				return false, fmt.Errorf("%w: %w", ErrMaxRetriesReached, err)
			}
			return attempt < listPricesMaxRetries, err
		}

		allPrices = prices
		return false, nil
	})

	if err != nil {
		p.logger.LogAttrs(ctx, slog.LevelError, "error populating volume prices",
			slog.String("err", err.Error()))
		return
	}

	p.logger.LogAttrs(ctx, slog.LevelDebug, "found volume prices",
		slog.Int("numOfPrices", len(allPrices)))

	if len(allPrices) == 0 {
		p.logger.LogAttrs(ctx, slog.LevelWarn, "no prices returned from API")
		return
	}

	p.volumePriceByRegionLock.Lock()
	defer p.volumePriceByRegionLock.Unlock()

	// Create a new map to replace the old one
	newPriceMap := make(VolumePriceByRegion)

	for _, price := range allPrices {
		regionName := price.ArmRegionName
		if regionName == "" {
			p.logger.LogAttrs(ctx, slog.LevelDebug, "region name for price not found",
				slog.String("sku", price.SkuName),
				slog.String("productName", price.ProductName))
			continue
		}

		if !validateVolumePriceIsRelevantFromSku(price) {
			p.logger.LogAttrs(ctx, slog.LevelDebug, "sku does not belong to a disk",
				slog.String("sku", price.SkuName),
				slog.String("productName", price.ProductName),
			)
			continue
		}

		// TODO: Why are standard sdd's not making it's way here
		if _, ok := newPriceMap[regionName]; !ok {
			p.logger.LogAttrs(ctx, slog.LevelDebug, "populating volume prices for region",
				slog.String("region", regionName))
			newPriceMap[regionName] = make(VolumePriceByProductName)
		}

		volumeSku := &VolumeSku{
			RetailPrice: price.RetailPrice,
			SkuName:     price.SkuName,
			ArmSkuName:  price.ArmSkuName,
		}

		if _, ok := newPriceMap[regionName][price.ProductName]; !ok {
			newPriceMap[regionName][price.ProductName] = make(VolumePriceBySkuName)
		}
		if len(price.ArmSkuName) == 0 {
			newPriceMap[regionName][price.ProductName][price.ArmSkuName] = volumeSku
		} else {
			newPriceMap[regionName][price.ProductName][price.SkuName] = volumeSku
		}

	}

	// Replace the old map with the new one
	p.VolumePriceByRegion = newPriceMap

	p.logger.LogAttrs(ctx, slog.LevelInfo, "volume price store populated",
		slog.Duration("duration", time.Since(startTime)))
}

// GetEstimatedMonthlyCost returns the estimated monthly cost for a disk
func (p *VolumePriceStore) GetEstimatedMonthlyCost(disk *armcompute.Disk) (float64, error) {
	region := string(*disk.Location)
	volumeSku, err := p.GetVolumePrice(region, string(*disk.SKU.Name))
	if err != nil {
		return 0, err
	}

	monthlyPrice := volumeSku.RetailPrice * float64(*disk.Properties.DiskSizeGB)
	return monthlyPrice, nil
}

var diskToPricingMap = map[string]string{
	// Standard HDD
	"Standard_LRS": "Standard HDD Managed Disk",
	// Standard HDD
	// Standard SSD
	"StandardSSD_LRS": "Standard SSD Managed Disk",
	"StandardSSD_ZRS": "Standard SSD Managed Disk",
	// Premium SSD
	"PremiumSSD_LRS": "Premium SSD Managed Disk",
	"PremiumSSD_ZRS": "Premium SSD Managed Disk",

	// Ultra SSD
	"UltraSSD": "Ultra SSD",
}

func standardizeSkuNameFromDisk(diskName string) string {
	if _, ok := diskToPricingMap[diskName]; !ok {
		return ""
	}

	return diskToPricingMap[diskName]
}

func getZoneTypeFromSkuName(diskname string) string {
	split := strings.Split(diskname, "_")
	if len(split) != 2 {
		return ""
	}
	return split[1]
}
