package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"log/slog"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute"
	"github.com/grafana/cloudcost-exporter/pkg/azure/aks"
	"github.com/grafana/cloudcost-exporter/pkg/azure/azureClientWrapper"
)

func main() {
	// Create a new Azure credential
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		log.Fatalf("failed to obtain a credential: %v", err)
	}

	// Create a new context
	ctx := context.Background()

	// Create a new client to list disks
	client, err := armcompute.NewDisksClient(os.Getenv("AZURE_SUBSCRIPTION_ID"), cred, nil)
	if err != nil {
		log.Fatalf("failed to create disks client: %v", err)
	}

	// Create a logger
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	// Create the price store
	priceClient, err := azureClientWrapper.NewAzureClientWrapper(logger, os.Getenv("AZURE_SUBSCRIPTION_ID"), cred)
	if err != nil {
		log.Fatalf("failed to create Azure client: %v", err)
	}
	priceStore := aks.NewVolumePriceStore(ctx, logger, priceClient)

	// Wait for price store to be populated
	for len(priceStore.VolumePriceByRegion) == 0 {
		time.Sleep(1 * time.Second)
	}

	// List all disks in the subscription
	pager := client.NewListPager(nil)
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			log.Printf("Error getting next page of disks: %v", err)
			continue
		}

		for _, disk := range page.Value {
			fmt.Println("------")
			if disk.Location != nil && disk.SKU.Name != nil {
				// Get pricing information
				price, err := priceStore.GetEstimatedMonthlyCost(disk)
				if err != nil {
					fmt.Printf("Price: Unable to fetch (%v)\n", err)
				} else {
					fmt.Printf("Disk: %s\n", *disk.Name)
					fmt.Printf("SKU: %s\n", *disk.SKU.Name)
					fmt.Printf("Tier: %s\n", *disk.SKU.Tier)
					fmt.Printf("Size: %d\n", *disk.Properties.DiskSizeGB)
					fmt.Printf("Estimated Monthly Cost: $%.2f\n", price)
				}
			}

			fmt.Println("------")
		}
	}
}
