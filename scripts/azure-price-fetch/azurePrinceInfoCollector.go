package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v4"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
	"github.com/Azure/go-autorest/autorest/to"
	"gomodules.xyz/azure-retail-prices-sdk-for-go/sdk"
)

type VirtualMachineInfo struct {
	Name            string
	OwningVMSS      string
	MachineTypeSku  string
	MachineTypeName string
	Spot            bool
	RetailPrice     float64
}

type VmMap struct {
	RegionMap map[string]map[string]VirtualMachineInfo
}

type PriceMap struct {
	RegionMap map[string]map[string]sdk.ResourceSKU
}

type AzurePriceInformationCollector struct {
	subscriptionId string

	priceClient  *sdk.RetailPricesClient
	rgClient     *armresources.ResourceGroupsClient
	vmssClient   *armcompute.VirtualMachineScaleSetsClient
	vmssVmClient *armcompute.VirtualMachineScaleSetVMsClient

	vmMap    *VmMap
	priceMap *PriceMap
}

func NewAzurePriceInformationCollector(subId string, cred *azidentity.DefaultAzureCredential) (*AzurePriceInformationCollector, error) {
	retailPricesClient, err := sdk.NewRetailPricesClient(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create retail prices client: %w", err)
	}

	rgClient, err := armresources.NewResourceGroupsClient(subId, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build resource group client: %w", err)
	}

	computeClientFactory, err := armcompute.NewClientFactory(subId, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build compute client: %w", err)
	}

	return &AzurePriceInformationCollector{
		subscriptionId: subId,

		priceClient:  retailPricesClient,
		rgClient:     rgClient,
		vmssClient:   computeClientFactory.NewVirtualMachineScaleSetsClient(),
		vmssVmClient: computeClientFactory.NewVirtualMachineScaleSetVMsClient(),

		vmMap:    &VmMap{RegionMap: make(map[string]map[string]VirtualMachineInfo)},
		priceMap: &PriceMap{RegionMap: make(map[string]map[string]sdk.ResourceSKU)},
	}, nil
}

func (a *AzurePriceInformationCollector) buildQueryFilter(locationList []string) string {
	locationListFilter := []string{}
	for _, t := range locationList {
		locationListFilter = append(locationListFilter, fmt.Sprintf("armRegionName eq '%s'", t))
	}

	locationListStr := strings.Join(locationListFilter, " or ")
	return fmt.Sprintf(`serviceName eq 'Virtual Machines' and priceType eq 'Consumption' and (%s)`, locationListStr)
}

func (a *AzurePriceInformationCollector) getPrices(ctx context.Context, locationList []string) error {
	pager := a.priceClient.NewListPager(&sdk.RetailPricesClientListOptions{
		APIVersion:  to.StringPtr("2023-01-01-preview"),
		Filter:      to.StringPtr(a.buildQueryFilter(locationList)),
		MeterRegion: to.StringPtr(`'primary'`),
	})

	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to advance page: %w", err)
		}
		for _, v := range page.Items {
			if _, ok := a.priceMap.RegionMap[v.ArmRegionName]; !ok {
				a.priceMap.RegionMap[v.ArmRegionName] = make(map[string]sdk.ResourceSKU)
			}
			a.priceMap.RegionMap[v.ArmRegionName][v.ArmSkuName] = v
		}
	}

	return nil
}

func (a *AzurePriceInformationCollector) getResourceGroupsAndLocationsInSubscription(ctx context.Context) (map[string]string, []string, error) {
	rgToLocationMap := make(map[string]string)
	locationSet := make(map[string]bool)
	uniqueLocationList := []string{}

	pager := a.rgClient.NewListPager(nil)
	for pager.More() {
		nextResult, err := pager.NextPage(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to advance page: %w", err)
		}

		for _, v := range nextResult.Value {
			rgToLocationMap[*v.Name] = *v.Location
			locationSet[*v.Location] = true
		}
	}

	for v := range locationSet {
		uniqueLocationList = append(uniqueLocationList, v)
	}
	return rgToLocationMap, uniqueLocationList, nil
}

func (a *AzurePriceInformationCollector) getVmInfoFromResourceGroup(ctx context.Context, rgName string) (map[string]VirtualMachineInfo, error) {
	vmInfoMap := map[string]VirtualMachineInfo{}

	pager := a.vmssClient.NewListPager(rgName, nil)
	for pager.More() {
		nextResult, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to advance page: %w", err)
		}

		for _, v := range nextResult.Value {
			vmsInfo, err := a.getVmInfoFromVmss(ctx, rgName, *v.Name)
			if err != nil {
				return nil, err
			}

			for vmName, vmInfo := range vmsInfo {
				vmInfoMap[vmName] = vmInfo
			}
		}
	}

	return vmInfoMap, nil
}

func (a *AzurePriceInformationCollector) getVmInfoFromVmss(ctx context.Context, rgName string, vmssName string) (map[string]VirtualMachineInfo, error) {
	vmInfo := make(map[string]VirtualMachineInfo)

	opts := &armcompute.VirtualMachineScaleSetVMsClientListOptions{}
	pager := a.vmssVmClient.NewListPager(rgName, vmssName, opts)
	for pager.More() {
		nextResult, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to advance page: %w", err)
		}

		for _, v := range nextResult.Value {
			vmInfo[*v.Name] = VirtualMachineInfo{
				Name:           *v.Name,
				OwningVMSS:     vmssName,
				MachineTypeSku: *v.SKU.Name,
			}
		}
	}

	return vmInfo, nil
}

func (a *AzurePriceInformationCollector) getRegionalVmInformationFromRgVmss(ctx context.Context, rgMap map[string]string) error {
	for rg, location := range rgMap {
		m, err := a.getVmInfoFromResourceGroup(ctx, rg)
		if err != nil {
			return err
		}

		if len(m) > 0 {
			if _, ok := a.vmMap.RegionMap[location]; !ok {
				a.vmMap.RegionMap[location] = make(map[string]VirtualMachineInfo)
			}
			a.vmMap.RegionMap[location] = m
		}
	}

	return nil
}

func (a *AzurePriceInformationCollector) enrichVmData() {
	for region, vmRegionInfo := range a.vmMap.RegionMap {
		for machineName, vmInfo := range vmRegionInfo {
			vmType := vmInfo.MachineTypeSku
			machineInfo := a.priceMap.RegionMap[region][vmType]

			vmInfo.RetailPrice = machineInfo.RetailPrice
			vmInfo.MachineTypeName = machineInfo.ProductName
			vmInfo.Spot = strings.Contains(machineInfo.SkuName, "Spot")

			a.vmMap.RegionMap[region][machineName] = vmInfo
		}
	}
}
