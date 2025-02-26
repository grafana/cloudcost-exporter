package aks

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/Azure/go-autorest/autorest/to"
	retailPriceSdk "gomodules.xyz/azure-retail-prices-sdk-for-go/sdk"

	"github.com/grafana/cloudcost-exporter/pkg/azure/azureClientWrapper"

	"gopkg.in/matryer/try.v1"
)

const (
	AzurePriceSearchFilter = `serviceName eq 'Storage' and priceType eq 'Consumption'`
	DefaultInstanceFamily  = "General purpose"

	MiBsToGiB = 1024
)

// cpuToCostRatio was generated by analysing Grafana Labs spend in GCP and finding the ratio of CPU to Memory spend by instance type.
// It's an imperfect approximation, but it's better than nothing.

// Note - these should be validated against live Azure data, but this is
// a servicable start.
var cpuToCostRatio = map[string]float64{
	"Compute optimized": 0.88,
	"Memory optimized":  0.48,
	"General purpose":   0.65,
	"Storage optimized": 0.48,
}

type MachinePrices struct {
	PricePerCore    float64
	PricePerGiB     float64
	PricePerDiskGiB float64
}

type MachineSku struct {
	RetailPrice float64

	MachinePricesBreakdown *MachinePrices
}

type PriceBySku map[string]*MachineSku

type PriceByOperatingSystem map[MachineOperatingSystem]PriceBySku

type PriceByPriority map[MachinePriority]PriceByOperatingSystem

type PriceStore struct {
	logger  *slog.Logger
	context context.Context

	azureClientWrapper azureClientWrapper.AzureClient

	machinePriceByPriorityLock *sync.RWMutex
	MachinePriceByPriority     map[string]PriceByPriority
}

func NewPricingStore(ctx context.Context, parentLogger *slog.Logger, azClientWrapper azureClientWrapper.AzureClient) *PriceStore {
	logger := parentLogger.With("subsystem", "priceStore")

	p := &PriceStore{
		logger:             logger,
		context:            ctx,
		azureClientWrapper: azClientWrapper,

		machinePriceByPriorityLock: &sync.RWMutex{},
		MachinePriceByPriority:     make(map[string]PriceByPriority),
	}

	// populate the store before it is used
	go p.PopulatePriceStore(ctx)

	return p
}

func (p *PriceStore) getPriceBreakdownFromVmInfo(vmInfo *VirtualMachineInfo, price float64) *MachinePrices {
	ratio, ok := cpuToCostRatio[vmInfo.MachineFamily]
	if !ok {
		p.logger.LogAttrs(p.context, slog.LevelInfo, "no ratio found for instance type, using default",
			slog.String("instanceType", vmInfo.MachineTypeSku),
			slog.String("instanceFamily", vmInfo.MachineFamily),
		)
		ratio = cpuToCostRatio[DefaultInstanceFamily]
	}

	return &MachinePrices{
		PricePerCore: price * ratio / float64(vmInfo.NumOfCores),
		PricePerGiB:  (price * (1 - ratio) / float64(vmInfo.MemoryInMiB)) * MiBsToGiB,
	}
}

func (p *PriceStore) getPriceInfoFromVmInfo(vmInfo *VirtualMachineInfo) (*MachineSku, error) {
	p.machinePriceByPriorityLock.RLock()
	defer p.machinePriceByPriorityLock.RUnlock()

	if vmInfo == nil {
		p.logger.Error("nil vm info passed into price map")
		return nil, ErrPriceInformationNotFound
	}

	region := vmInfo.Region
	priority := vmInfo.Priority
	operatingSystem := vmInfo.OperatingSystem
	sku := vmInfo.MachineTypeSku

	if len(region) == 0 || len(sku) == 0 {
		p.logger.LogAttrs(p.context, slog.LevelError, "region or sku not defined", slog.String("region", region), slog.String("sku", sku), slog.String("vmInfo", fmt.Sprintf("%+v", vmInfo)))
		return nil, ErrPriceInformationNotFound
	}

	rMap := p.MachinePriceByPriority[region]
	if rMap == nil {
		p.logger.LogAttrs(p.context, slog.LevelError, "region not found in price map", slog.String("region", region))
		return nil, ErrPriceInformationNotFound
	}

	pMap := rMap[priority]
	if pMap == nil {
		p.logger.LogAttrs(p.context, slog.LevelError, "priority not found in region map", slog.String("region", region), slog.String("priority", priority.String()), slog.String("vmInfo", fmt.Sprintf("%+v", vmInfo)))
		return nil, ErrPriceInformationNotFound
	}

	osMap := pMap[operatingSystem]
	if osMap == nil {
		p.logger.LogAttrs(p.context, slog.LevelError, "os map not found in priority map", slog.String("os", operatingSystem.String()), slog.String("vmInfo", fmt.Sprintf("%+v", vmInfo)))
		return nil, ErrPriceInformationNotFound
	}

	machineSku := osMap[sku]
	if machineSku == nil {
		p.logger.LogAttrs(p.context, slog.LevelError, "sku info not found in os map", slog.String("sku", sku), slog.String("vmInfo", fmt.Sprintf("%+v", vmInfo)))
		return nil, ErrPriceInformationNotFound
	}

	if machineSku.MachinePricesBreakdown == nil {
		prices := p.getPriceBreakdownFromVmInfo(vmInfo, float64(machineSku.RetailPrice))
		machineSku.MachinePricesBreakdown = prices
	}

	return machineSku, nil
}

// Note that while we could do this with the following filter in the search:
//
//	`serviceName eq 'Virtual Machines' and priceType eq 'Consumption' and contains(productName, "Virtual Machines") and (contains(skuName, "Low Priority") ne true)`
//
// We have observed that, in practice, this is _much_ slower than
// filtering client-side.  :(
func (p *PriceStore) validateMachinePriceIsRelevantFromSku(ctx context.Context, sku *retailPriceSdk.ResourceSKU) bool {
	productName := sku.ProductName
	if len(productName) == 0 || !strings.Contains(productName, "Virtual Machines") {
		p.logger.LogAttrs(ctx, slog.LevelDebug, "product is not a virtual machine", slog.String("sku", sku.SkuName))
		return false
	}

	skuName := sku.SkuName
	if len(skuName) == 0 || strings.Contains(skuName, "Low Priority") {
		p.logger.LogAttrs(ctx, slog.LevelDebug, "disregarding low priority machines", slog.String("sku", sku.SkuName))
		return false
	}

	return true
}

func (p *PriceStore) PopulatePriceStore(ctx context.Context) {
	startTime := time.Now()

	p.logger.Info("populating price store")

	opts := &retailPriceSdk.RetailPricesClientListOptions{
		APIVersion:  to.StringPtr(AZ_API_VERSION),
		Filter:      to.StringPtr(AzurePriceSearchFilter),
		MeterRegion: to.StringPtr(AzureMeterRegion),
	}

	var prices []*retailPriceSdk.ResourceSKU
	err := try.Do(func(attempt int) (bool, error) {
		var err error
		prices, err = p.azureClientWrapper.ListPrices(ctx, opts)
		if attempt == listPricesMaxRetries && err != nil {
			return false, fmt.Errorf("%w: %w", ErrMaxRetriesReached, err)
		}
		return attempt < listPricesMaxRetries, err
	})

	if err != nil {
		p.logger.LogAttrs(ctx, slog.LevelError, "error populating prices", slog.String("err", err.Error()))
		return
	}

	p.logger.LogAttrs(ctx, slog.LevelDebug, "found prices", slog.Int("numOfPrices", len(prices)))

	// Clear out price store only if we have new price data
	p.machinePriceByPriorityLock.Lock()
	defer p.machinePriceByPriorityLock.Unlock()
	clear(p.MachinePriceByPriority)

	for _, price := range prices {
		regionName := price.ArmRegionName
		if regionName == "" {
			p.logger.LogAttrs(ctx, slog.LevelDebug, "region name for price not found", slog.String("sku", price.SkuName))
			continue
		}

		if !p.validateMachinePriceIsRelevantFromSku(ctx, price) {
			continue
		}

		if _, ok := p.MachinePriceByPriority[regionName]; !ok {
			p.logger.LogAttrs(ctx, slog.LevelDebug, "populating machine prices for region", slog.String("region", regionName))
			p.MachinePriceByPriority[regionName] = make(PriceByPriority)
			p.MachinePriceByPriority[regionName][Spot] = make(PriceByOperatingSystem)
			p.MachinePriceByPriority[regionName][OnDemand] = make(PriceByOperatingSystem)
		}

		machineOperatingSystem := getMachineOperatingSystemFromSku(price)
		machinePriority := getMachinePriorityFromSku(price)

		if _, ok := p.MachinePriceByPriority[regionName][machinePriority][machineOperatingSystem]; !ok {
			p.MachinePriceByPriority[regionName][machinePriority][machineOperatingSystem] = make(PriceBySku)
		}

		machinePrices := &MachineSku{
			RetailPrice: price.RetailPrice,
		}
		p.MachinePriceByPriority[regionName][machinePriority][machineOperatingSystem][price.ArmSkuName] = machinePrices
	}

	p.logger.LogAttrs(ctx, slog.LevelInfo, "price store populated", slog.Duration("duration", time.Since(startTime)))
}

func getMachineOperatingSystemFromSku(sku *retailPriceSdk.ResourceSKU) MachineOperatingSystem {
	switch {
	case strings.Contains(sku.ProductName, "Windows"):
		return Windows
	default:
		return Linux
	}
}

func getMachinePriorityFromSku(sku *retailPriceSdk.ResourceSKU) MachinePriority {
	switch {
	case strings.Contains(sku.SkuName, "Spot"):
		return Spot
	default:
		return OnDemand
	}
}
