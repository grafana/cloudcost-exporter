package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

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
}

type PricingApiResponse struct {
	Items        []PricingDetails `json:"Items"`
	NextPageLink string           `json:"NextPageLink"`
}

type PriceMap map[string]PricingDetails

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
		(*pm)[price.SkuId] = price
	}
}

func generatePricingMapFromApi() {
	apiUrl := "https://prices.azure.com/api/retail/prices?api-version=2023-01-01-preview&$filter=serviceFamily eq 'Compute' and priceType eq 'Consumption'"
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

	fmt.Println(priceMap)
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

	for pager.More() {
		nextResult, err := pager.NextPage(ctx)
		if err != nil {
			log.Fatalf("failed to advance page: %v", err)
		}

		for _, v := range nextResult.Value {
			_ = v
		}
	}

	generatePricingMapFromApi()
	fmt.Println("hello world")
}
