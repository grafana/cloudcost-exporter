package billing

import (
	"context"
	"errors"
	"fmt"
	"strings"

	billingv1 "cloud.google.com/go/billing/apiv1"
	"cloud.google.com/go/billing/apiv1/billingpb"
	"google.golang.org/api/iterator"
)

var ServiceNotFound = errors.New("service not found")

// GetServiceName will search for a service by the display name and return the full name.
// The full name is need by the GetPricing method to collect all the pricing information for a given service.
func GetServiceName(ctx context.Context, billingService *billingv1.CloudCatalogClient, name string) (string, error) {
	serviceIterator := billingService.ListServices(ctx, &billingpb.ListServicesRequest{PageSize: 5000})
	for {
		service, err := serviceIterator.Next()
		if err != nil {
			if errors.Is(err, iterator.Done) {
				break
			}
			return "", err
		}
		if service.DisplayName == name {
			return service.Name, nil
		}
	}
	return "", ServiceNotFound
}

// GetPricing will collect all the pricing information for a given service and return a list of skus.
func GetPricing(ctx context.Context, billingService *billingv1.CloudCatalogClient, serviceName string) []*billingpb.Sku {
	var skus []*billingpb.Sku
	skuIterator := billingService.ListSkus(ctx, &billingpb.ListSkusRequest{Parent: serviceName})
	for {
		sku, err := skuIterator.Next()
		if err != nil {
			if errors.Is(err, iterator.Done) {
				break
			}
			// keep going if we get an error
		}

		// We don't include licensing skus in our pricing map
		if !strings.Contains(strings.ToLower(sku.Description), "licensing") {

			skus = append(skus, sku)
		} else {
			if sku.Category.ResourceFamily == "Storage" {
				fmt.Printf("%s:%s:%s:%0.4f\n", sku.Description, sku.Category.ResourceGroup, sku.ServiceRegions[0], float64(sku.PricingInfo[0].PricingExpression.TieredRates[0].UnitPrice.Nanos)*1e-9)
				skus = append(skus, sku)
			}
		}
	}
	return skus
}
