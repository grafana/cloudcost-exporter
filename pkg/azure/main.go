package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v4"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"

	"github.com/Azure/go-autorest/autorest/to"
	"gomodules.xyz/azure-retail-prices-sdk-for-go/sdk"
)

type PricingDetails struct {
	RetailPrice   float64 `json:"retailPrice"`
	Region        string  `json:"armRegionName"`
	ProductId     string  `json:"productId"`
	SkuId         string  `json:"skuId"`
	Type          string  `json:"type"`
	ServiceFamily string  `json:"serviceFamily"`
	ArmSkuName    string  `json:"armSkuName"`
}

type PricingApiResponse struct {
	Items        []PricingDetails `json:"Items"`
	NextPageLink string           `json:"NextPageLink"`
}

type VmScaleSetInfo struct {
	Name     string
	Capacity int
	TypeSku  string
	Location string
}

type TotalPriceInfo struct {
	Info       VmScaleSetInfo
	PriceTotal float64
}

type PriceMap struct {
	RegionMap map[string]map[string]PricingDetails
}

func getLocationsFromSubscriptionResourceGroups(ctx context.Context, subId string, cred *azidentity.DefaultAzureCredential) []string {
	activeLocations := map[string]bool{}

	rgClientFactory, err := armresources.NewClientFactory(subId, cred, nil)
	if err != nil {
		log.Fatal(err)
	}

	rgClient := rgClientFactory.NewClient()
	pager := rgClient.NewListPager(nil)

	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			log.Fatal(err)
		}

		if page.ResourceListResult.Value != nil {
			for _, v := range page.ResourceListResult.Value {
				loc := to.String(v.Location)
				activeLocations[loc] = true
			}
		}
	}

	activeLocationsList := []string{}
	for v := range activeLocations {
		activeLocationsList = append(activeLocationsList, v)
	}

	return activeLocationsList
}

func getPrices(ctx context.Context, locationList []string) *PriceMap {
	pm := PriceMap{
		RegionMap: make(map[string]map[string]PricingDetails),
	}

	client, err := sdk.NewRetailPricesClient(nil)
	if err != nil {
		panic(err)
	}

	locationListFilter := []string{}
	for _, t := range locationList {
		locationListFilter = append(locationListFilter, fmt.Sprintf("armRegionName eq '%s'", t))
	}
	queryFilterString := strings.Join(locationListFilter, " or ")

	pager := client.NewListPager(&sdk.RetailPricesClientListOptions{
		APIVersion:  to.StringPtr("2023-01-01-preview"), // 2023-01-01-preview
		Filter:      to.StringPtr(fmt.Sprintf(`serviceName eq 'Virtual Machines' and priceType eq 'Consumption' and (%s)`, queryFilterString)),
		MeterRegion: to.StringPtr(`'primary'`),
	})

	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			panic(err)
		}
		for _, v := range page.Items {
			reg := v.ArmRegionName
			skuName := v.ArmSkuName

			pd := PricingDetails{
				RetailPrice: v.RetailPrice,
				Region:      reg,
				ArmSkuName:  skuName,
			}

			if _, ok := pm.RegionMap[reg]; !ok {
				pm.RegionMap[reg] = make(map[string]PricingDetails)
			}
			pm.RegionMap[reg][skuName] = pd
		}
	}

	return &pm
}

func getVmScaleSetInfo(ctx context.Context, subId string, cred *azidentity.DefaultAzureCredential) map[string]VmScaleSetInfo {
	vmScaleSetInfoMap := map[string]VmScaleSetInfo{}

	computeClientFactory, err := armcompute.NewClientFactory(subId, cred, nil)
	if err != nil {
		log.Fatal(err)
	}

	virtualMachineScaleSetsClient := computeClientFactory.NewVirtualMachineScaleSetsClient()

	pager := virtualMachineScaleSetsClient.NewListAllPager(nil)

	for pager.More() {
		nextResult, err := pager.NextPage(ctx)
		if err != nil {
			log.Fatalf("failed to advance page: %v", err)
		}

		for _, v := range nextResult.Value {
			vmScaleSetInfoMap[*v.Name] = VmScaleSetInfo{
				Name:     *v.Name,
				Location: *v.Location,
				Capacity: int(*v.SKU.Capacity),
				TypeSku:  *v.SKU.Name,
			}
		}
	}

	return vmScaleSetInfoMap
}

func main() {
	ctx := context.TODO()
	subscriptionID := os.Getenv("AZURE_SUBSCRIPTION_ID")
	if subscriptionID == "" {
		log.Fatal("no subscription id specified")
	}

	credential, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		panic(err)
	}

	vmScaleSetInfoMap := getVmScaleSetInfo(ctx, subscriptionID, credential)

	locationsForSubscription := getLocationsFromSubscriptionResourceGroups(ctx, subscriptionID, credential)
	pm := getPrices(ctx, locationsForSubscription)

	totalPricesMap := map[string]TotalPriceInfo{}

	for vmName, info := range vmScaleSetInfoMap {
		pricePerType := pm.RegionMap[info.Location][info.TypeSku].RetailPrice
		totalPrice := pricePerType * float64(info.Capacity)
		totalPricesMap[vmName] = TotalPriceInfo{
			Info:       info,
			PriceTotal: totalPrice,
		}
	}

	for name, info := range totalPricesMap {
		fmt.Printf("VMSS %s, info: %+v\n\n", name, info)
	}
}
