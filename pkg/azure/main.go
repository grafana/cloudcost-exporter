package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v4"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/costmanagement/armcostmanagement"
)

type PricingDetails struct {
	RetailPrice   float32 `json:"retailPrice"`
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

type PriceMap map[string]map[string]PricingDetails

var computeClientFactory *armcompute.ClientFactory
var costClientFactory *armcostmanagement.ClientFactory

func parseResponse(p *PricingApiResponse, pm *PriceMap) {
	for _, price := range p.Items {
		if price.Type != "Consumption" {
			continue
		}
		if price.ServiceFamily != "Compute" {
			continue
		}
		if _, ok := (*pm)[price.Region]; !ok {
			(*pm)[price.Region] = map[string]PricingDetails{}
		}
		(*pm)[price.Region][price.ArmSkuName] = price
	}
}

func generatePricingMapFromApi(machineTypes []string) PriceMap {
	machineTypesStringFilters := []string{}

	for _, t := range machineTypes {
		machineTypesStringFilters = append(machineTypesStringFilters, fmt.Sprintf("armSkuName eq '%s'", t))
	}
	queryFilterString := strings.Join(machineTypesStringFilters, " or ")

	apiUrl := fmt.Sprintf("https://prices.azure.com/api/retail/prices?api-version=2023-01-01-preview&$filter=serviceFamily eq 'Compute' and priceType eq 'Consumption' and %s", queryFilterString)
	priceMap := PriceMap{}

	for {
		res, err := http.Get(apiUrl)
		if err != nil {
			log.Fatal(err)
		}
		defer res.Body.Close()
		body, err := io.ReadAll(res.Body)
		if err != nil {
			log.Fatal(err)
		}

		var result PricingApiResponse
		err = json.Unmarshal(body, &result)
		if err != nil {
			log.Fatal(err)
		}

		parseResponse(&result, &priceMap)

		if result.NextPageLink == "" {
			break
		}

		apiUrl = result.NextPageLink
	}

	return priceMap
}

func main() {
	subscriptionID := os.Getenv("AZURE_SUBSCRIPTION_ID")
	if subscriptionID == "" {
		log.Fatal("no subscription id specified")
	}
	ctx := context.TODO()

	credential, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		panic(err)
	}

	computeClientFactory, err = armcompute.NewClientFactory(subscriptionID, credential, nil)
	if err != nil {
		log.Fatal(err)
	}

	virtualMachineScaleSetsClient := computeClientFactory.NewVirtualMachineScaleSetsClient()

	opts := armcompute.VirtualMachineScaleSetsClientListAllOptions{}
	pager := virtualMachineScaleSetsClient.NewListAllPager(&opts)

	vmScaleSetInfoMap := map[string]VmScaleSetInfo{}

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

	machineTypes := map[string]bool{}
	for _, info := range vmScaleSetInfoMap {
		machineTypes[info.TypeSku] = true
	}
	machineTypeNames := []string{}
	for i := range machineTypes {
		machineTypeNames = append(machineTypeNames, i)
	}

	pm := generatePricingMapFromApi(machineTypeNames)

	type TotalPriceInfo struct {
		Info       VmScaleSetInfo
		PriceTotal float32
	}
	totalPricesMap := map[string]TotalPriceInfo{}

	for vmName, info := range vmScaleSetInfoMap {
		pricePerType := pm[info.Location][info.TypeSku].RetailPrice
		totalPrice := pricePerType * float32(info.Capacity)
		totalPricesMap[vmName] = TotalPriceInfo{
			Info:       info,
			PriceTotal: totalPrice,
		}
	}

	for name, info := range totalPricesMap {
		fmt.Printf("VMSS %s, cost total: %v\n", name, info.PriceTotal)
	}
}
