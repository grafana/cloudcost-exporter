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
	lock              *sync.RWMutex
	subscriptionId    string
	logger            *slog.Logger
	context           context.Context
	retailPriceClient *retailPriceSdk.RetailPricesClient

	RegionMap map[string]PriceByPriority
}

func NewPricingStore(parentContext context.Context, parentLogger *slog.Logger, subId string) (*PriceStore, error) {
	logger := parentLogger.With("subsystem", "priceStore")

	retailPricesClient, err := retailPriceSdk.NewRetailPricesClient(nil)
	if err != nil {
		logger.LogAttrs(parentContext, slog.LevelError, "failed to create retail prices client", slog.String("err", err.Error()))
		return nil, ErrClientCreationFailure
	}

	p := &PriceStore{
		lock:              &sync.RWMutex{},
		logger:            logger,
		context:           parentContext,
		subscriptionId:    subId,
		retailPriceClient: retailPricesClient,

		RegionMap: make(map[string]PriceByPriority),
	}

	go func() {
		err := p.PopulatePriceStore([]string{})
		if err != nil {
			p.logger.LogAttrs(p.context, slog.LevelError, "error populating initial price store", slog.String("error", err.Error()))
		}
	}()

	return p, err
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

	p.lock.Lock()
	defer p.lock.Unlock()

	pager := p.retailPriceClient.NewListPager(p.buildListOptions(locationList))

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
				p.logger.LogAttrs(p.context, slog.LevelInfo, "populating machine prices for region", slog.String("region", regionName))
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

// TODO - implement ability to lookup a certain VM's
// Price by it's ID
func (p *PriceStore) GetVmPrice() {}

// TODO - use to grab regional prices
// func (p *PriceStore) getPricesByRegion(region string) (*PriceByPriority, error) {
// 	p.lock.RLock()
// 	defer p.lock.RUnlock()

// 	priceByPriority, ok := p.RegionMap[region]
// 	if !ok {
// 		return nil, fmt.Errorf("region %s not found", region)
// 	}

// 	return &priceByPriority, nil
// }