package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/go-autorest/autorest/to"
	"golang.org/x/sync/errgroup"
	"gomodules.xyz/azure-retail-prices-sdk-for-go/sdk"
)

func main() {
	ctx := context.TODO()
	subscriptionID := os.Getenv("AZURE_SUBSCRIPTION_ID")
	if subscriptionID == "" {
		log.Fatal("no subscription id specified")
	}

	credential, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		log.Fatal(err)
	}

	azureClientWrapper, err := NewAzureClientWrapper(ctx, subscriptionID, credential)
	if err != nil {
		log.Fatal(err)
	}

	rgLocationMap, uniqueLocationList, err := azureClientWrapper.getResourceGroupsAndLocationsInSubscription(ctx)
	if err != nil {
		log.Fatal(err)
	}

	vmMap := &VmMap{}
	priceMap := &PriceMap{}

	eg, newCtx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		vmMap, err = azureClientWrapper.getRegionalVmInformationFromRgVmss(newCtx, rgLocationMap)
		return err
	})

	eg.Go(func() error {
		priceMap, err = azureClientWrapper.getPrices(newCtx, uniqueLocationList)
		return err
	})

	err = eg.Wait()
	if err != nil {
		log.Fatal(err)
	}

	debugSummary(priceMap, vmMap)
}

type VirtualMachineInfo struct {
	Name        string
	OwningVMSS  string
	MachineType string
}

type VmMap struct {
	RegionMap map[string]map[string]VirtualMachineInfo
}

type PriceMap struct {
	RegionMap map[string]map[string]sdk.ResourceSKU
}

func (a *AzureClientWrapper) buildQueryFilter(locationList []string) string {
	locationListFilter := []string{}
	for _, t := range locationList {
		locationListFilter = append(locationListFilter, fmt.Sprintf("armRegionName eq '%s'", t))
	}

	locationListStr := strings.Join(locationListFilter, " or ")
	return fmt.Sprintf(`serviceName eq 'Virtual Machines' and priceType eq 'Consumption' and (%s)`, locationListStr)
}

func (a *AzureClientWrapper) getPrices(ctx context.Context, locationList []string) (*PriceMap, error) {
	pm := PriceMap{
		RegionMap: make(map[string]map[string]sdk.ResourceSKU),
	}

	pager := a.priceClient.NewListPager(&sdk.RetailPricesClientListOptions{
		APIVersion:  to.StringPtr("2023-01-01-preview"),
		Filter:      to.StringPtr(a.buildQueryFilter(locationList)),
		MeterRegion: to.StringPtr(`'primary'`),
	})

	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to advance page: %w", err)
		}
		for _, v := range page.Items {
			if _, ok := pm.RegionMap[v.ArmRegionName]; !ok {
				pm.RegionMap[v.ArmRegionName] = make(map[string]sdk.ResourceSKU)
			}
			pm.RegionMap[v.ArmRegionName][v.ArmSkuName] = v
		}
	}

	return &pm, nil
}

func (a *AzureClientWrapper) getResourceGroupsAndLocationsInSubscription(ctx context.Context) (map[string]string, []string, error) {
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

func (a *AzureClientWrapper) getVmInfoFromResourceGroup(ctx context.Context, rgName string) (map[string]VirtualMachineInfo, error) {
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

func (a *AzureClientWrapper) getVmInfoFromVmss(ctx context.Context, rgName string, vmssName string) (map[string]VirtualMachineInfo, error) {
	vmInfo := make(map[string]VirtualMachineInfo)

	pager := a.vmssVmClient.NewListPager(rgName, vmssName, nil)
	for pager.More() {
		nextResult, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to advance page: %w", err)
		}

		for _, v := range nextResult.Value {
			vmInfo[*v.Name] = VirtualMachineInfo{
				Name:        *v.Name,
				OwningVMSS:  vmssName,
				MachineType: *v.SKU.Name,
			}
		}
	}

	return vmInfo, nil
}

func (a *AzureClientWrapper) getRegionalVmInformationFromRgVmss(ctx context.Context, rgMap map[string]string) (*VmMap, error) {
	vmMap := VmMap{
		RegionMap: make(map[string]map[string]VirtualMachineInfo),
	}

	for rg, location := range rgMap {
		m, err := a.getVmInfoFromResourceGroup(ctx, rg)
		if err != nil {
			return nil, err
		}

		if len(m) > 0 {
			if _, ok := vmMap.RegionMap[location]; !ok {
				vmMap.RegionMap[location] = make(map[string]VirtualMachineInfo)
			}
			vmMap.RegionMap[location] = m
		}
	}

	return &vmMap, nil
}

func debugSummary(priceMap *PriceMap, vmMap *VmMap) {
	type Summary struct {
		RegionName    string
		TotalCost     float64
		TotalMachines int
		MachineTypes  map[string]bool
	}
	totalHourlyCostPerRegion := map[string]Summary{}

	for region, vmInformation := range vmMap.RegionMap {
		fmt.Printf("Prices for region: %s\n", region)
		totalCost := float64(0.0)
		totalMachines := 0
		machineTypes := map[string]bool{}

		for vmName, vmInfo := range vmInformation {
			vmType := vmInfo.MachineType
			vmPrice := priceMap.RegionMap[region][vmType].RetailPrice
			totalCost += vmPrice
			totalMachines++
			machineTypes[vmInfo.MachineType] = true

			fmt.Printf("Prices for vm %s of type %s in region %s: %v\n", vmName, vmType, region, vmPrice)
		}

		totalHourlyCostPerRegion[region] = Summary{
			RegionName:    region,
			TotalCost:     totalCost,
			TotalMachines: totalMachines,
			MachineTypes:  machineTypes,
		}
	}

	for r, c := range totalHourlyCostPerRegion {
		fmt.Printf("Total Cost per hour of the Region %s: %+v\n", r, c)
	}
}
