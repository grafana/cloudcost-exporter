package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/Azure/go-autorest/autorest/to"
	retailPriceSdk "gomodules.xyz/azure-retail-prices-sdk-for-go/sdk"
)

type PricingResponse struct {
	Items        []retailPriceSdk.ResourceSKU `json:"Items"`
	NextPageLink string                       `json:"NextPageLink"`
	Count        int                          `json:"Count"`
}

func main() {
	// Output files
	outputFile := "azure_all_prices.json"

	// Create the retail prices client
	client, err := retailPriceSdk.NewRetailPricesClient(nil)
	if err != nil {
		fmt.Printf("Error creating client: %v\n", err)
		os.Exit(1)
	}

	var allItems []retailPriceSdk.ResourceSKU
	pageCount := 0

	// Initial options - no filter to get everything
	options := &retailPriceSdk.RetailPricesClientListOptions{
		Filter: to.StringPtr("serviceName eq 'Storage'"),
	}

	pager := client.NewListPager(options)
	for pager.More() {
		pageCount++
		fmt.Printf("Fetching page %d...\n", pageCount)

		page, err := pager.NextPage(context.Background())
		if err != nil {
			fmt.Printf("Error getting page: %v\n", err)
			break
		}

		allItems = append(allItems, page.Items...)
		time.Sleep(500 * time.Millisecond)
	}

	// Save all items to file
	result := PricingResponse{
		Items: allItems,
		Count: len(allItems),
	}

	jsonData, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fmt.Printf("Error marshaling JSON: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile(outputFile, jsonData, 0644); err != nil {
		fmt.Printf("Error writing to file: %v\n", err)
		os.Exit(1)
	}

	// Extract unique service names
	fmt.Println("Extracting unique service names...")
	serviceMap := make(map[string]bool)
	for _, item := range allItems {
		serviceMap[item.ServiceName] = true
	}
}
