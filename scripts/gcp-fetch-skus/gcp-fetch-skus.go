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

	billingv1 "cloud.google.com/go/billing/apiv1"

	"github.com/grafana/cloudcost-exporter/pkg/google/billing"
)

type Config struct {
	Service    string
	OutputFile string
}

func main() {
	var config *Config
	flag.StringVar(&config.Service, "service", "Compute Engine", "The service to fetch skus for")
	flag.StringVar(&config.OutputFile, "output-file", "skus.csv", "The file to write the skus to")
	flag.Parse()
	if err := run(config); err != nil {
		log.Printf("error: %v", err)
		os.Exit(1)
	}
}

func run(config *Config) error {
	ctx := context.Background()
	client, err := billingv1.NewCloudCatalogClient(ctx)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()
	svcid, err := billing.GetServiceName(ctx, client, config.Service)
	if err != nil {
		log.Fatal(err)
	}
	skus := billing.GetPricing(ctx, client, svcid)
	file, err := os.Create(config.OutputFile)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()
	writer := csv.NewWriter(file)
	err = writer.Write([]string{"sku_id", "description", "category", "region", "pricing_info"})
	if err != nil {
		return fmt.Errorf("error writing record to csv: %v", err)
	}
	skusWithMultipleRates := map[string]int{}
	for _, sku := range skus {
		for _, region := range sku.ServiceRegions {
			price := ""
			if len(sku.PricingInfo) != 0 {
				if len(sku.PricingInfo[0].PricingExpression.TieredRates) != 0 {
					rates := len(sku.PricingInfo[0].PricingExpression.TieredRates)
					price = strconv.FormatFloat(float64(sku.PricingInfo[0].PricingExpression.TieredRates[0].UnitPrice.Nanos)*1e-9, 'f', -1, 64)
					if rates > 1 {
						if sku.Category.ResourceFamily == "Storage" {
							fmt.Printf("Family %s has %d rates\n", sku.Description, rates)
						}
						skusWithMultipleRates[sku.Category.ResourceFamily] = skusWithMultipleRates[sku.Category.ResourceFamily] + 1
						price = strconv.FormatFloat(float64(sku.PricingInfo[0].PricingExpression.TieredRates[rates-1].UnitPrice.Nanos)*1e-9, 'f', -1, 64)
					}
				}
			}
			err = writer.Write([]string{sku.SkuId, sku.Description, sku.Category.ResourceFamily, region, price})
			if err != nil {
				return fmt.Errorf("error writing record to csv: %v", err)
			}
		}
	}
	writer.Flush()
	return writer.Error()
}
