package aks

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/Azure/go-autorest/autorest/to"
	retailPriceSdk "gomodules.xyz/azure-retail-prices-sdk-for-go/sdk"
)

var (
	ErrPriceInformationNotFound = errors.New("price information not found in map")
)

type PriceBySku map[string]*retailPriceSdk.ResourceSKU

type PriceByOperatingSystem map[MachineOperatingSystem]PriceBySku

type PriceByPriority map[MachinePriority]PriceByOperatingSystem

type PriceStore struct {
	subscriptionId    string
	logger            *slog.Logger
	context           context.Context
	retailPriceClient *retailPriceSdk.RetailPricesClient

	regionMapLock *sync.RWMutex
	RegionMap     map[string]PriceByPriority
}

func NewPricingStore(parentContext context.Context, parentLogger *slog.Logger, subId string) (*PriceStore, error) {
	logger := parentLogger.With("subsystem", "priceStore")

	retailPricesClient, err := retailPriceSdk.NewRetailPricesClient(nil)
	if err != nil {
		logger.LogAttrs(parentContext, slog.LevelError, "failed to create retail prices client", slog.String("err", err.Error()))
		return nil, ErrClientCreationFailure
	}

	p := &PriceStore{
		logger:            logger,
		context:           parentContext,
		subscriptionId:    subId,
		retailPriceClient: retailPricesClient,

		regionMapLock: &sync.RWMutex{},
		RegionMap:     make(map[string]PriceByPriority),
	}

	go func() {
		// populate the store before it is used
		err := p.PopulatePriceStore(p.context)
		if err != nil {
			// if it fails, subsequent calls to Collect() will populate the store
			p.logger.LogAttrs(p.context, slog.LevelError, "error populating initial price store", slog.String("error", err.Error()))
		}
	}()

	return p, err
}

func (p *PriceStore) getPriceInfoFromVmInfo(vmInfo *VirtualMachineInfo) (float64, error) {
	p.regionMapLock.RLock()
	defer p.regionMapLock.RUnlock()

	if vmInfo == nil {
		p.logger.Error("nil vm info passed into price map")
		return 0.0, ErrPriceInformationNotFound
	}

	region := vmInfo.Region
	priority := vmInfo.Priority
	operatingSystem := vmInfo.OperatingSystem
	sku := vmInfo.MachineTypeSku

	if len(region) == 0 || len(sku) == 0 {
		p.logger.LogAttrs(p.context, slog.LevelError, "region or sku not defined", slog.String("region", region), slog.String("sku", sku))
		return 0.0, ErrPriceInformationNotFound
	}

	rMap := p.RegionMap[region]
	if rMap == nil {
		p.logger.LogAttrs(p.context, slog.LevelError, "region not found in price map", slog.String("region", region))
		return 0.0, ErrPriceInformationNotFound
	}

	pMap := rMap[priority]
	if pMap == nil {
		p.logger.LogAttrs(p.context, slog.LevelError, "priority not found in region map", slog.String("region", region), slog.String("priority", priority.String()))
		return 0.0, ErrPriceInformationNotFound
	}

	osMap := pMap[operatingSystem]
	if osMap == nil {
		p.logger.LogAttrs(p.context, slog.LevelError, "os map not found in priority map", slog.String("os", operatingSystem.String()))
		return 0.0, ErrPriceInformationNotFound
	}

	skuInfo := osMap[sku]
	if skuInfo == nil {
		p.logger.LogAttrs(p.context, slog.LevelError, "sku info not found in os map", slog.String("sku", sku))
		return 0.0, ErrPriceInformationNotFound
	}

	return skuInfo.RetailPrice, nil
}

func (p *PriceStore) PopulatePriceStore(ctx context.Context) error {
	startTime := time.Now()
	p.logger.Info("populating price store")

	p.regionMapLock.Lock()
	defer p.regionMapLock.Unlock()

	clear(p.RegionMap)

	opts := &retailPriceSdk.RetailPricesClientListOptions{
		APIVersion:  to.StringPtr(AZ_API_VERSION),
		Filter:      to.StringPtr(`serviceName eq 'Virtual Machines' and priceType eq 'Consumption'`),
		MeterRegion: to.StringPtr(`'primary'`),
	}

	pager := p.retailPriceClient.NewListPager(opts)
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			p.logger.LogAttrs(ctx, slog.LevelError, "error paging through retail prices")
			return ErrPageAdvanceFailure
		}

		for _, v := range page.Items {
			regionName := v.ArmRegionName
			if regionName == "" {
				p.logger.LogAttrs(ctx, slog.LevelInfo, "region name for price not found", slog.String("sku", v.SkuName))
				continue
			}

			if _, ok := p.RegionMap[regionName]; !ok {
				p.logger.LogAttrs(ctx, slog.LevelDebug, "populating machine prices for region", slog.String("region", regionName))
				p.RegionMap[regionName] = make(PriceByPriority)
				p.RegionMap[regionName][Spot] = make(PriceByOperatingSystem)
				p.RegionMap[regionName][OnDemand] = make(PriceByOperatingSystem)
			}

			machineOperatingSystem := getMachineOperatingSystemFromSku(v)
			machinePriority := getMachinePriorityFromSku(v)

			if _, ok := p.RegionMap[regionName][machinePriority][machineOperatingSystem]; !ok {
				p.RegionMap[regionName][machinePriority][machineOperatingSystem] = make(PriceBySku)
			}
			p.RegionMap[regionName][machinePriority][machineOperatingSystem][v.ArmSkuName] = &v
		}
	}

	p.logger.LogAttrs(ctx, slog.LevelInfo, "price store populated", slog.Duration("duration", time.Since(startTime)))
	return nil
}

func getMachineOperatingSystemFromSku(sku retailPriceSdk.ResourceSKU) MachineOperatingSystem {
	switch {
	case strings.Contains(sku.ProductName, "Windows"):
		return Windows
	default:
		return Linux
	}
}

func getMachinePriorityFromSku(sku retailPriceSdk.ResourceSKU) MachinePriority {
	switch {
	case strings.Contains(sku.SkuName, "Spot"):
		return Spot
	default:
		return OnDemand
	}

}
