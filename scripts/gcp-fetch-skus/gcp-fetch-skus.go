// Usage: go run gcp-fetch-skus.go
// THis is a useful utility if you want to fetch a set of sku's for a particular service and export them to CSV.
package main

import (
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"

	client2 "github.com/grafana/cloudcost-exporter/pkg/google/client"
)

type Config struct {
	Service    string
	OutputFile string
}

func main() {
	var config Config
	flag.StringVar(&config.Service, "service", "Compute Engine", "The service to fetch skus for")
	flag.StringVar(&config.OutputFile, "output-file", "skus.csv", "The file to write the skus to")
	flag.Parse()
	if err := run(&config); err != nil {
		log.Printf("error: %v", err)
		os.Exit(1)
	}
}

func run(config *Config) error {
	ctx := context.Background()
	gcpClient, err := client2.NewGCPClient(ctx, client2.Config{
		ProjectId: "",
		Discount:  0,
	})
	if err != nil {
		return err
	}

	svcid, err := gcpClient.GetServiceName(ctx, config.Service)
	if err != nil {
		log.Fatal(err)
	}
	skus := gcpClient.GetPricing(ctx, svcid)
	file, err := os.Create(config.OutputFile)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()
	writer := csv.NewWriter(file)
	err = writer.Write([]string{"sku_id", "description", "category", "region", "pricing_info"})
	if err != nil {
		return fmt.Errorf("error writing record to csv: %w", err)
	}
	for _, sku := range skus {
		for _, region := range sku.ServiceRegions {
			price := ""
			if len(sku.PricingInfo) != 0 {
				if len(sku.PricingInfo[0].PricingExpression.TieredRates) != 0 {
					rates := len(sku.PricingInfo[0].PricingExpression.TieredRates)
					rateIdx := 0
					if rates > 1 {
						rateIdx = rates - 1
					}
					price = strconv.FormatFloat(float64(sku.PricingInfo[0].PricingExpression.TieredRates[rateIdx].UnitPrice.Nanos)*1e-9, 'f', -1, 64)
				}
			}
			err = writer.Write([]string{sku.SkuId, sku.Description, sku.Category.ResourceFamily, region, price})
			if err != nil {
				return fmt.Errorf("error writing record to csv: %w", err)
			}
		}
	}
	writer.Flush()
	return writer.Error()
}
