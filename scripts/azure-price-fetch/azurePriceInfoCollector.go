package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v4"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
	"github.com/Azure/go-autorest/autorest/to"
	"gomodules.xyz/azure-retail-prices-sdk-for-go/sdk"
)

const AZ_API_VERSION string = "2023-01-01-preview" // using latest API Version https://learn.microsoft.com/en-us/rest/api/cost-management/retail-prices/azure-retail-prices
var ErrClientCreationFailure = errors.New("failed to create client")

type VirtualMachineInfo struct {
	Name            string
	OwningVMSS      string
	MachineTypeSku  string
	MachineTypeName string
	OperatingSystem string
	Spot            bool
	RetailPrice     float64
}

type VmMap struct {
	RegionMap map[string]map[string]VirtualMachineInfo
}
type PriceByPriority struct {
	SpotPrices    map[string]map[string]sdk.ResourceSKU
	RegularPrices map[string]map[string]sdk.ResourceSKU
}

type PriceMap struct {
	RegionMap map[string]PriceByPriority
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
		return nil, ErrClientCreationFailure
	}

	rgClient, err := armresources.NewResourceGroupsClient(subId, cred, nil)
	if err != nil {
		return nil, ErrClientCreationFailure
	}

	computeClientFactory, err := armcompute.NewClientFactory(subId, cred, nil)
	if err != nil {
		return nil, ErrClientCreationFailure
	}

	return &AzurePriceInformationCollector{
		subscriptionId: subId,

		priceClient:  retailPricesClient,
		rgClient:     rgClient,
		vmssClient:   computeClientFactory.NewVirtualMachineScaleSetsClient(),
		vmssVmClient: computeClientFactory.NewVirtualMachineScaleSetVMsClient(),

		vmMap:    &VmMap{RegionMap: make(map[string]map[string]VirtualMachineInfo)},
		priceMap: &PriceMap{RegionMap: make(map[string]PriceByPriority)},
	}, nil
}

func (a *AzurePriceInformationCollector) buildQueryFilter(locationList []string) string {
	locationListFilter := []string{}
	for _, region := range locationList {
		locationListFilter = append(locationListFilter, fmt.Sprintf("armRegionName eq '%s'", region))
	}

	locationListStr := strings.Join(locationListFilter, " or ")
	return fmt.Sprintf(`serviceName eq 'Virtual Machines' and priceType eq 'Consumption' and (%s)`, locationListStr)
}

func (a *AzurePriceInformationCollector) getPrices(ctx context.Context, locationList []string) error {
	pager := a.priceClient.NewListPager(&sdk.RetailPricesClientListOptions{
		APIVersion:  to.StringPtr(AZ_API_VERSION),
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
				a.priceMap.RegionMap[v.ArmRegionName] = PriceByPriority{
					SpotPrices:    make(map[string]map[string]sdk.ResourceSKU),
					RegularPrices: make(map[string]map[string]sdk.ResourceSKU),
				}
			}

			spot := strings.Contains(v.SkuName, "Spot")
			osKey := "Linux"
			if strings.Contains(v.ProductName, "Windows") {
				osKey = "Windows"
			}

			if spot {
				if _, ok := a.priceMap.RegionMap[v.ArmRegionName].SpotPrices[osKey]; !ok {
					a.priceMap.RegionMap[v.ArmRegionName].SpotPrices[osKey] = make(map[string]sdk.ResourceSKU)
				}
				a.priceMap.RegionMap[v.ArmRegionName].SpotPrices[osKey][v.ArmSkuName] = v
			} else {
				if _, ok := a.priceMap.RegionMap[v.ArmRegionName].RegularPrices[osKey]; !ok {
					a.priceMap.RegionMap[v.ArmRegionName].RegularPrices[osKey] = make(map[string]sdk.ResourceSKU)
				}
				a.priceMap.RegionMap[v.ArmRegionName].RegularPrices[osKey][v.ArmSkuName] = v
			}
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
			osInfo := "Windows"
			spot := false
			if v.Properties.VirtualMachineProfile.Priority != nil && *v.Properties.VirtualMachineProfile.Priority == armcompute.VirtualMachinePriorityTypesSpot {
				spot = true
			}
			if v.Properties.VirtualMachineProfile.OSProfile != nil && v.Properties.VirtualMachineProfile.OSProfile.LinuxConfiguration != nil {
				osInfo = "Linux"
			}
			vmsInfo, err := a.getVmInfoFromVmss(ctx, rgName, *v.Name, spot, osInfo)
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

func (a *AzurePriceInformationCollector) getVmInfoFromVmss(ctx context.Context, rgName string, vmssName string, vmssIsSpot bool, osInfo string) (map[string]VirtualMachineInfo, error) {
	vmInfo := make(map[string]VirtualMachineInfo)

	opts := &armcompute.VirtualMachineScaleSetVMsClientListOptions{
		Expand: to.StringPtr("instanceView"),
	}
	pager := a.vmssVmClient.NewListPager(rgName, vmssName, opts)
	for pager.More() {
		nextResult, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to advance page: %w", err)
		}

		for _, v := range nextResult.Value {
			vmInfo[*v.Name] = VirtualMachineInfo{
				Name:            *v.Name,
				OwningVMSS:      vmssName,
				MachineTypeSku:  *v.SKU.Name,
				Spot:            vmssIsSpot,
				OperatingSystem: osInfo,
			}
		}
	}

	return vmInfo, nil
}

func (a *AzurePriceInformationCollector) getRegionalVmInformationFromRgVmss(ctx context.Context, rgMap map[string]string) error {
	for rg, location := range rgMap {
		virtualMachineInfoByRg, err := a.getVmInfoFromResourceGroup(ctx, rg)
		if err != nil {
			return err
		}

		if len(virtualMachineInfoByRg) > 0 {
			if _, ok := a.vmMap.RegionMap[location]; !ok {
				a.vmMap.RegionMap[location] = make(map[string]VirtualMachineInfo)
			}
			a.vmMap.RegionMap[location] = virtualMachineInfoByRg
		}
	}

	return nil
}

func (a *AzurePriceInformationCollector) enrichVmData() {
	for region, vmRegionInfo := range a.vmMap.RegionMap {
		for machineName, vmInfo := range vmRegionInfo {
			var machineInfo sdk.ResourceSKU

			vmType := vmInfo.MachineTypeSku
			spot := vmInfo.Spot
			os := vmInfo.OperatingSystem
			if spot {
				machineInfo = a.priceMap.RegionMap[region].SpotPrices[os][vmType]
			} else {
				machineInfo = a.priceMap.RegionMap[region].RegularPrices[os][vmType]
			}

			vmInfo.RetailPrice = machineInfo.RetailPrice
			vmInfo.MachineTypeName = machineInfo.ProductName

			a.vmMap.RegionMap[region][machineName] = vmInfo
		}
	}
}
