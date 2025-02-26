package aks

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/Azure/go-autorest/autorest/to"
	"github.com/grafana/cloudcost-exporter/pkg/azure/azureClientWrapper"
	retailPriceSdk "gomodules.xyz/azure-retail-prices-sdk-for-go/sdk"
	"gopkg.in/matryer/try.v1"
)

const (
	AzureVolumePriceSearchFilter = `serviceName eq 'Storage' and contains(productName, 'Disk') and priceType eq 'Consumption'`
	AzureMeterRegion             = `'primary'`
	AZ_API_VERSION               = "2023-01-01-preview"
	listPricesMaxRetries         = 5
	priceRefreshInterval         = 24 * time.Hour
	monthlyUnitOfMeasure         = "1/Month"
)

var (
	ErrPriceInformationNotFound = errors.New("price information not found in map")
	ErrMaxRetriesReached        = errors.New("max retries reached")
)

type VolumeSku struct {
	RetailPrice float64
	SkuName     string
	Tier        string
}

type PriceBySku map[string]*VolumeSku
type PriceByRegion map[string]PriceBySku

type VolumePriceStore struct {
	logger  *slog.Logger
	context context.Context

	volumePriceByRegionLock *sync.RWMutex
	VolumePriceByRegion     PriceByRegion

	// Use the existing interface
	azureClient azureClientWrapper.AzureClient
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
		VolumePriceByRegion:     make(PriceByRegion),
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

	return volumeSku, nil
}

// validateVolumePriceIsRelevantFromSku checks if the price is relevant for persistent volumes
// It checks if the product name contains "Disk" and if the unit of measure is per month
func (p *VolumePriceStore) validateVolumePriceIsRelevantFromSku(ctx context.Context, sku *retailPriceSdk.ResourceSKU) bool {
	productName := sku.ProductName
	if len(productName) == 0 || !strings.Contains(productName, "Disk") {
		p.logger.LogAttrs(ctx, slog.LevelDebug, "product is not a disk",
			slog.String("sku", sku.SkuName))
		return false
	}

	// Only include per-month pricing
	if sku.UnitOfMeasure != monthlyUnitOfMeasure {
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
	newPriceMap := make(PriceByRegion)

	for _, price := range allPrices {
		regionName := price.ArmRegionName
		if regionName == "" {
			p.logger.LogAttrs(ctx, slog.LevelDebug, "region name for price not found",
				slog.String("sku", price.SkuName))
			continue
		}

		if !p.validateVolumePriceIsRelevantFromSku(ctx, price) {
			continue
		}

		if _, ok := newPriceMap[regionName]; !ok {
			p.logger.LogAttrs(ctx, slog.LevelDebug, "populating volume prices for region",
				slog.String("region", regionName))
			newPriceMap[regionName] = make(PriceBySku)
		}

		volumeSku := &VolumeSku{
			RetailPrice: price.RetailPrice,
			SkuName:     price.ArmSkuName,
			Tier:        price.SkuName,
		}

		newPriceMap[regionName][price.ArmSkuName] = volumeSku
	}

	// Replace the old map with the new one
	p.VolumePriceByRegion = newPriceMap

	p.logger.LogAttrs(ctx, slog.LevelInfo, "volume price store populated",
		slog.Duration("duration", time.Since(startTime)))
}

// GetEstimatedMonthlyCost returns the estimated monthly cost for a disk
func (p *VolumePriceStore) GetEstimatedMonthlyCost(region string, skuName string, sizeGB int) (float64, error) {
	volumeSku, err := p.GetVolumePrice(region, skuName)
	if err != nil {
		return 0, err
	}

	monthlyPrice := volumeSku.RetailPrice * float64(sizeGB)
	return monthlyPrice, nil
}
