package main

import (
	"context"
	"encoding/csv"
	"fmt"
	"log"
	"os"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute"
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

	// List all disks in the subscription
	pager := client.NewListPager(nil)
	var disks []*armcompute.Disk
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			log.Printf("Error getting next page of disks: %v", err)
			continue
		}
		disks = append(disks, page.Value...)
	}
	if err := writeDiskDataToCSV(disks, "disks.csv"); err != nil {
		log.Fatalf("Error writing out data: %s", err.Error())
	}
}

func writeDiskDataToCSV(disks []*armcompute.Disk, outputPath string) error {
	file, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	if err := writer.Write([]string{"name", "location", "sku", "tier", "size_gb"}); err != nil {
		return fmt.Errorf("failed to write out header: %w", err)
	}
	for _, disk := range disks {
		size := "0"
		if disk.Properties != nil && disk.Properties.DiskSizeGB != nil {
			size = fmt.Sprintf("%d", *disk.Properties.DiskSizeGB)
		}
		skuName := ""
		if disk.SKU != nil && disk.SKU.Name != nil {
			skuName = string(*disk.SKU.Name)
		}

		location := ""
		if disk.Location != nil {
			location = *disk.Location
		}

		name := ""
		if disk.Name != nil {
			name = *disk.Name
		}
		tier := ""
		if disk.Properties.Tier != nil {
			tier = *disk.Properties.Tier
		}

		record := []string{name, location, skuName, size, tier}
		if err := writer.Write(record); err != nil {
			return fmt.Errorf("failed to write record: %w", err)
		}
	}
	return nil
}
