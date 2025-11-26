package client

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	billingv1 "cloud.google.com/go/billing/apiv1"
	"cloud.google.com/go/billing/apiv1/billingpb"
	"github.com/grafana/cloudcost-exporter/pkg/google/metrics"
	"google.golang.org/api/iterator"
)

// ServiceNotFound indicates the requested GCP service was not found in the Cloud Catalog.
var errServiceNotFound = errors.New("service not found")

var (
	// errTaggingNotSupported indicates that tagging SKUs are not supported by the exporter.
	errTaggingNotSupported = errors.New("tagging sku's is not supported")
	// errInvalidSKU indicates that a SKU didn’t provide valid pricing information.
	errInvalidSKU = errors.New("invalid sku")
	// errUnknownPricingUnit indicates an unrecognized pricing unit description.
	errUnknownPricingUnit = errors.New("unknown pricing unit")

	gibMonthly = "gibibyte month"
	gibDay     = "gibibyte day"
)

type Billing struct {
	billingService *billingv1.CloudCatalogClient
}

func newBilling(billingService *billingv1.CloudCatalogClient) *Billing {
	return &Billing{
		billingService: billingService,
	}
}

// getServiceName will search for a service by the display name and return the full name.
// The full name is need by the GetPricing method to collect all the pricing information for a given service.
func (b *Billing) getServiceName(ctx context.Context, name string) (string, error) {
	serviceIterator := b.billingService.ListServices(ctx, &billingpb.ListServicesRequest{PageSize: 5000})
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
	return "", errServiceNotFound
}

func (b *Billing) exportBilling(ctx context.Context, serviceName string, m *metrics.Metrics) float64 {
	skus := b.getPricing(ctx, serviceName)
	for _, sku := range skus {
		// Skip Egress and Download costs as we don't count them yet
		// Check category first as I've had random segfaults locally
		if sku.Category != nil && sku.Category.ResourceFamily == "Network" {
			continue
		}
		if strings.HasSuffix(sku.Description, "Data Retrieval") {
			continue
		}
		if sku.Description == "Autoclass Management Fee" {
			continue
		}
		if sku.Description == "Bucket Tagging Storage" {
			continue
		}
		if strings.HasSuffix(sku.Category.ResourceGroup, "Storage") {
			if strings.Contains(sku.Description, "Early Delete") {
				continue // to skip "Unknown sku"
			}
			if err := parseStorageSku(sku, m); err != nil {
				log.Printf("error parsing storage sku: %v", err)
			}
			continue
		}
		if strings.HasSuffix(sku.Category.ResourceGroup, "Ops") {
			if err := parseOpSku(sku, m); err != nil {
				log.Printf("error parsing op sku: %v", err)
			}
			continue
		}
		log.Printf("Unknown sku: %s\n", sku.Description)
	}
	return 1.0
}

// getPricing will collect all the pricing information for a given service and return a list of skus.
func (b *Billing) getPricing(ctx context.Context, serviceName string) []*billingpb.Sku {
	var skus []*billingpb.Sku
	skuIterator := b.billingService.ListSkus(ctx, &billingpb.ListSkusRequest{Parent: serviceName})
	for {
		sku, err := skuIterator.Next()
		if err != nil {
			if errors.Is(err, iterator.Done) {
				break
			}
			log.Println(err) // keep going if we get an error
		}
		skus = append(skus, sku)
	}
	return skus
}

func getPriceFromSku(sku *billingpb.Sku) (float64, error) {
	// TODO: Do we need to support Multiple PricingInfo?
	// That not needed here as we query only actual pricing
	if len(sku.PricingInfo) < 1 {
		return 0.0, fmt.Errorf("%w:%s", errInvalidSKU, sku.Description)
	}
	priceInfo := sku.PricingInfo[0]

	// PricingInfo could have several Costs Tiers.
	// From observed data when there are several tiers first tiers are "free tiers",
	// so we should return actual prices.
	tierRatesLen := len(priceInfo.PricingExpression.TieredRates)
	if tierRatesLen < 1 {
		return 0.0, fmt.Errorf("found sku without TieredRates: %+v", sku)
	}
	tierRate := priceInfo.PricingExpression.TieredRates[tierRatesLen-1]
	// The cost of the SKU is units + nanos
	return float64(tierRate.UnitPrice.Units) + 1e-9*float64(tierRate.UnitPrice.Nanos), nil // Convert NanoUSD to USD when return
}

func parseStorageSku(sku *billingpb.Sku, m *metrics.Metrics) error {
	price, err := getPriceFromSku(sku)
	if err != nil {
		return err
	}
	priceInfo := sku.PricingInfo[0]
	priceUnit := priceInfo.PricingExpression.UsageUnitDescription

	// Adjust price to hourly
	switch priceUnit {
	case gibMonthly:
		price = price / 31 / 24
	case gibDay:
		// For Early-Delete in Archive, CloudStorage and Nearline classes
		price = price / 24
	default:
		return fmt.Errorf("%w:%s, %s", errUnknownPricingUnit, sku.Description, priceUnit)
	}

	region := regionNameSameAsStackdriver(sku.ServiceRegions[0])
	storageclass := storageClassFromSkuDescription(sku.Description, region)
	m.StorageGauge.WithLabelValues(region, storageclass).Set(price)
	return nil
}

func parseOpSku(sku *billingpb.Sku, m *metrics.Metrics) error {
	if strings.Contains(sku.Description, "Tagging") {
		return errTaggingNotSupported
	}

	price, err := getPriceFromSku(sku)
	if err != nil {
		return err
	}

	region := regionNameSameAsStackdriver(sku.ServiceRegions[0])
	storageclass := storageClassFromSkuDescription(sku.Description, region)
	opclass := opClassFromSkuDescription(sku.Description)

	m.OperationsGauge.WithLabelValues(region, storageclass, opclass).Set(price)
	return nil
}

// regionNameSameAsStackdriver will normalize region collectorName to be the same as what Stackdriver uses.
// Google Cost API returns region names exactly the same how they are referred in StackDriver metrics except one case:
// For Europe multi-region:
// API returns "europe", while Stackdriver uses "eu" label value.
func regionNameSameAsStackdriver(s string) string {
	if s == "europe" {
		return "eu"
	}
	return s
}

// opClassFromSkuDescription normalizes sku description to one of the following:
// - If the opsclass contains Class A, it's "class-a"
// - If the opsclass contains Class B, it's "class-b"
// - Otherwise, return the original opsclass
func opClassFromSkuDescription(s string) string {
	if strings.Contains(s, "Class A") {
		return "class-a"
	} else if strings.Contains(s, "Class B") {
		return "class-b"
	}
	return s
}

// storageClassFromSkuDescription normalize sku description to match the output from stackdriver exporter
func storageClassFromSkuDescription(s string, region string) string {
	if strings.Contains(s, "Coldline") {
		return "COLDLINE"
	} else if strings.Contains(s, "Nearline") {
		return "NEARLINE"
	} else if strings.Contains(s, "Durable Reduced Availability") {
		return "DRA"
	} else if strings.Contains(s, "Archive") {
		return "ARCHIVE"
	} else if strings.Contains(s, "Dual-Region") || strings.Contains(s, "Dual-region") {
		// Iowa and South Carolina regions (us-central1 and us-east1) are using "REGIONAL"
		// in billing and pricing, but sku.description state SKU as "Dual-region"
		if region == "us-central1" || region == "us-east1" {
			return "REGIONAL"
		}
		return "MULTI_REGIONAL"
	} else if strings.Contains(s, "Multi-Region") || strings.Contains(s, "Multi-region") {
		return "MULTI_REGIONAL"
	} else if strings.Contains(s, "Regional") || strings.Contains(s, "Storage") || strings.Contains(s, "Standard") {
		return "REGIONAL"
	}
	return s
}
