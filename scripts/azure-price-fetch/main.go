package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"golang.org/x/sync/errgroup"
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

	azureClientWrapper, err := NewAzureClientWrapper(subscriptionID, credential)
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
