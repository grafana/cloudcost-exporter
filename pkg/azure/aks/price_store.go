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
)

type PriceBySku map[string]retailPriceSdk.ResourceSKU

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
		err := p.PopulatePriceStore([]string{})
		if err != nil {
			p.logger.LogAttrs(p.context, slog.LevelError, "error populating initial price store", slog.String("error", err.Error()))
		}
	}()

	return p, err
}

func (p *PriceStore) getPriceInfoFromVmInfo(vmInfo *VirtualMachineInfo) (float64, error) {
	p.regionMapLock.RLock()
	defer p.regionMapLock.RUnlock()

	region := vmInfo.Region
	priority := vmInfo.Priority
	operatingSystem := vmInfo.OperatingSystem
	sku := vmInfo.MachineTypeSku

	vmPriceInfo := p.RegionMap[region][priority][operatingSystem][sku]

	return vmPriceInfo.RetailPrice, nil
}

func (p *PriceStore) buildQueryFilter(locationList []string) string {
	if len(locationList) == 0 {
		return `serviceName eq 'Virtual Machines' and priceType eq 'Consumption'`
	}

	locationListFilter := []string{}
	for _, region := range locationList {
		locationListFilter = append(locationListFilter, fmt.Sprintf("armRegionName eq '%s'", region))
	}

	locationListStr := strings.Join(locationListFilter, " or ")
	return fmt.Sprintf(`serviceName eq 'Virtual Machines' and priceType eq 'Consumption' and (%s)`, locationListStr)
}

func (p *PriceStore) buildListOptions(locationList []string) *retailPriceSdk.RetailPricesClientListOptions {
	queryFilter := p.buildQueryFilter(locationList)
	return &retailPriceSdk.RetailPricesClientListOptions{
		APIVersion:  to.StringPtr(AZ_API_VERSION),
		Filter:      to.StringPtr(queryFilter),
		MeterRegion: to.StringPtr(`'primary'`),
	}
}

func (p *PriceStore) PopulatePriceStore(locationList []string) error {
	startTime := time.Now()
	p.logger.Info("populating price store")

	p.regionMapLock.Lock()
	defer p.regionMapLock.Unlock()

	// clear the existing region map
	p.RegionMap = make(map[string]PriceByPriority)

	pager := p.retailPriceClient.NewListPager((p.buildListOptions(locationList)))
	for pager.More() {
		page, err := pager.NextPage(p.context)
		if err != nil {
			p.logger.LogAttrs(p.context, slog.LevelError, "error paging")
			return ErrPageAdvanceFailure
		}

		for _, v := range page.Items {
			regionName := v.ArmRegionName
			if regionName == "" {
				p.logger.LogAttrs(p.context, slog.LevelInfo, "region name for price not found", slog.String("sku", v.SkuName))
				continue
			}

			if _, ok := p.RegionMap[regionName]; !ok {
				p.logger.LogAttrs(p.context, slog.LevelDebug, "populating machine prices for region", slog.String("region", regionName))
				p.RegionMap[regionName] = make(PriceByPriority)
				p.RegionMap[regionName][Spot] = make(PriceByOperatingSystem)
				p.RegionMap[regionName][OnDemand] = make(PriceByOperatingSystem)
			}

			machineOperatingSystem := determineMachineOperatingSystem(v)
			machinePriority := determineMachinePriority(v)

			if _, ok := p.RegionMap[regionName][machinePriority][machineOperatingSystem]; !ok {
				p.RegionMap[regionName][machinePriority][machineOperatingSystem] = make(PriceBySku)
			}
			p.RegionMap[regionName][machinePriority][machineOperatingSystem][v.ArmSkuName] = v
		}
	}

	p.logger.LogAttrs(p.context, slog.LevelInfo, "price store populated", slog.Duration("duration", time.Since(startTime)))
	return nil
}

func determineMachineOperatingSystem(sku retailPriceSdk.ResourceSKU) MachineOperatingSystem {
	switch {
	case strings.Contains(sku.ProductName, "Windows"):
		return Windows
	default:
		return Linux
	}
}

func determineMachinePriority(sku retailPriceSdk.ResourceSKU) MachinePriority {
	switch {
	case strings.Contains(sku.SkuName, "Spot"):
		return Spot
	default:
		return OnDemand
	}

}
