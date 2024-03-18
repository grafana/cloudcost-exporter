package main

import (
	"context"
	"encoding/csv"
	"fmt"
	"log"
	"os"
	"strconv"

	billingv1 "cloud.google.com/go/billing/apiv1"

	"github.com/grafana/cloudcost-exporter/pkg/google/billing"
)

func main() {
	ctx := context.Background()
	client, err := billingv1.NewCloudCatalogClient(ctx)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()
	svcid, err := billing.GetServiceName(ctx, client, "Compute Engine")
	if err != nil {
		log.Fatal(err)
	}
	skus := billing.GetPricing(ctx, client, svcid)
	file, err := os.Create("skus-with-license.csv")
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()
	writer := csv.NewWriter(file)
	writer.Write([]string{"sku_id", "description", "category", "region", "pricing_info"})
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
			writer.Write([]string{sku.SkuId, sku.Description, sku.Category.ResourceFamily, region, price})
		}
	}
	writer.Flush()
	if writer.Error() != nil {
		log.Fatal(writer.Error())
	}
}
