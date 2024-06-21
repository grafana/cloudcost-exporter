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

	a, err := NewAzurePriceInformationCollector(subscriptionID, credential)
	if err != nil {
		log.Fatal(err)
	}

	rgLocationMap, uniqueLocationList, err := a.getResourceGroupsAndLocationsInSubscription(ctx)
	if err != nil {
		log.Fatal(err)
	}

	eg, newCtx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		return a.getRegionalVmInformationFromRgVmss(newCtx, rgLocationMap)
	})

	eg.Go(func() error {
		return a.getPrices(newCtx, uniqueLocationList)
	})

	err = eg.Wait()
	if err != nil {
		log.Fatal(err)
	}

	a.enrichVmData()
	debugSummary(a)
}

func debugSummary(a *AzurePriceInformationCollector) {
	type Summary struct {
		RegionName    string
		TotalCost     float64
		TotalMachines int
		MachineTypes  map[string]bool
	}
	totalHourlyCostPerRegion := map[string]Summary{}

	for region, vmInformation := range a.vmMap.RegionMap {
		fmt.Printf("Prices for region: %s\n", region)
		totalCost := float64(0.0)
		totalMachines := 0
		machineTypes := map[string]bool{}

		for vmName, vmInfo := range vmInformation {
			totalCost += vmInfo.RetailPrice
			totalMachines++
			machineTypes[vmInfo.MachineTypeSku] = true

			fmt.Printf("Prices for vm %s of type %s and sku %s in region %s: %v\n", vmName, vmInfo.MachineTypeName, vmInfo.MachineTypeSku, region, vmInfo.RetailPrice)
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
