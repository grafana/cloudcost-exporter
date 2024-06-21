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
	"golang.org/x/sync/errgroup"

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

func getLocationsFromSubscriptionVMSS(ctx context.Context, subId string, cred *azidentity.DefaultAzureCredential) ([]string, error) {
	activeLocations := map[string]bool{}

	rgClientFactory, err := armresources.NewClientFactory(subId, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build resources client: %w", err)
	}

	rgClient := rgClientFactory.NewClient()
	opts := &armresources.ClientListOptions{
		Filter: to.StringPtr(`resourceType eq 'Microsoft.Compute/virtualMachineScaleSets'`),
	}
	pager := rgClient.NewListPager(opts)

	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to advance page: %w", err)
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

	return activeLocationsList, nil
}

func buildQueryFilter(locationList []string) string {
	locationListFilter := []string{}
	for _, t := range locationList {
		locationListFilter = append(locationListFilter, fmt.Sprintf("armRegionName eq '%s'", t))
	}

	locationListStr := strings.Join(locationListFilter, " or ")
	return fmt.Sprintf(`serviceName eq 'Virtual Machines' and priceType eq 'Consumption' and (%s)`, locationListStr)
}

func getPrices(ctx context.Context, locationList []string) (*PriceMap, error) {
	pm := PriceMap{
		RegionMap: make(map[string]map[string]PricingDetails),
	}

	client, err := sdk.NewRetailPricesClient(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create retail prices client: %w", err)
	}

	pager := client.NewListPager(&sdk.RetailPricesClientListOptions{
		APIVersion:  to.StringPtr("2023-01-01-preview"),
		Filter:      to.StringPtr(buildQueryFilter(locationList)),
		MeterRegion: to.StringPtr(`'primary'`),
	})

	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to advance page: %w", err)
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

	return &pm, nil
}

func getVmScaleSetInfo(ctx context.Context, subId string, cred *azidentity.DefaultAzureCredential) (map[string]VmScaleSetInfo, error) {
	vmScaleSetInfoMap := map[string]VmScaleSetInfo{}

	computeClientFactory, err := armcompute.NewClientFactory(subId, cred, nil)
	if err != nil {
		return nil, err
	}

	virtualMachineScaleSetsClient := computeClientFactory.NewVirtualMachineScaleSetsClient()
	pager := virtualMachineScaleSetsClient.NewListAllPager(nil)

	for pager.More() {
		nextResult, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to advance page: %w", err)
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

	return vmScaleSetInfoMap, nil
}

func makeTotalPrices(vmScaleSetInfoMap map[string]VmScaleSetInfo, priceInfoMap *PriceMap) map[string]TotalPriceInfo {
	totalPricesMap := map[string]TotalPriceInfo{}

	for vmName, info := range vmScaleSetInfoMap {
		pricePerType := priceInfoMap.RegionMap[info.Location][info.TypeSku].RetailPrice
		totalPrice := pricePerType * float64(info.Capacity)
		totalPricesMap[vmName] = TotalPriceInfo{
			Info:       info,
			PriceTotal: totalPrice,
		}
	}

	return totalPricesMap
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

	vmScaleSetInfoMap := make(map[string]VmScaleSetInfo)
	priceMap := &PriceMap{}

	eg, newCtx := errgroup.WithContext(ctx)

	eg.Go(func() error {
		vmScaleSetInfoMap, err = getVmScaleSetInfo(newCtx, subscriptionID, credential)
		return err
	})

	eg.Go(func() error {
		locationsForSubscription, err := getLocationsFromSubscriptionVMSS(ctx, subscriptionID, credential)
		if err != nil {
			return err
		}

		priceMap, err = getPrices(ctx, locationsForSubscription)
		return err
	})

	err = eg.Wait()
	if err != nil {
		log.Fatal(err)
	}

	totalPricesMap := makeTotalPrices(vmScaleSetInfoMap, priceMap)

	for name, info := range totalPricesMap {
		fmt.Printf("VMSS %s, info: %+v\n\n", name, info)
	}
}
