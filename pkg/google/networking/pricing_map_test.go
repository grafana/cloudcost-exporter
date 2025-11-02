package networking

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"testing"

	"cloud.google.com/go/billing/apiv1/billingpb"
	"github.com/grafana/cloudcost-exporter/pkg/google/client"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/type/money"
)

var testLogger = slog.New(slog.NewTextHandler(os.Stdout, nil))

// stubClient implements just the pricing-related parts of client.Client used by pricing_map.
type stubClient struct {
	client.Client
	skus []*billingpb.Sku
}

func (s *stubClient) GetServiceName(ctx context.Context, name string) (string, error) {
	return name, nil
}
func (s *stubClient) GetPricing(ctx context.Context, service string) []*billingpb.Sku { return s.skus }

func TestParse(t *testing.T) {
	// Irrelevant SKU (wrong resource group) should be ignored
	irrelevant := &billingpb.Sku{
		Category:    &billingpb.Category{ResourceGroup: "Compute"},
		Description: "Some other service",
	}

	// Relevant SKUs with LoadBalancing resource group for each description
	frSku := &billingpb.Sku{
		Category:    &billingpb.Category{ResourceGroup: ResourceGroup},
		Description: forwardingRuleDescription + " hourly",
		GeoTaxonomy: &billingpb.GeoTaxonomy{Regions: []string{"us-central1"}},
		PricingInfo: []*billingpb.PricingInfo{{
			PricingExpression: &billingpb.PricingExpression{
				TieredRates: []*billingpb.PricingExpression_TierRate{{
					UnitPrice: &money.Money{Nanos: 25000000}, // 0.025
				}},
			},
		}},
	}
	inSku := &billingpb.Sku{
		Category:    &billingpb.Category{ResourceGroup: ResourceGroup},
		Description: inboundDataProcessedDescription + " per GiB",
		GeoTaxonomy: &billingpb.GeoTaxonomy{Regions: []string{"us-central1"}},
		PricingInfo: []*billingpb.PricingInfo{{
			PricingExpression: &billingpb.PricingExpression{
				TieredRates: []*billingpb.PricingExpression_TierRate{{
					UnitPrice: &money.Money{Nanos: 120000000}, // 0.12
				}},
			},
		}},
	}
	outSku := &billingpb.Sku{
		Category:    &billingpb.Category{ResourceGroup: ResourceGroup},
		Description: outboundDataProcessedDescription + " per GiB",
		GeoTaxonomy: &billingpb.GeoTaxonomy{Regions: []string{"us-central1"}},
		PricingInfo: []*billingpb.PricingInfo{{
			PricingExpression: &billingpb.PricingExpression{
				TieredRates: []*billingpb.PricingExpression_TierRate{{
					UnitPrice: &money.Money{Nanos: 110000000}, // 0.11
				}},
			},
		}},
	}
	pm := &pricingMap{
		pricing: make(map[string]*pricing),
		logger:  testLogger,
	}
	skuData, err := pm.parseSku(
		[]*billingpb.Sku{irrelevant, frSku, inSku, outSku})
	require.NoError(t, err)
	require.Len(t, skuData, 3)
	require.NoError(t, pm.processSkuData(skuData))
	skuDataB, _ := json.Marshal(skuData)
	fmt.Println(string(skuDataB))
	pmPricingB, _ := json.Marshal(pm.pricing)
	fmt.Println(string(pmPricingB))
	price, err := pm.GetCostOfForwardingRule("us-central1")
	require.NoError(t, err)
	// Expect forwarding rule price
	require.Equal(t, 0.025, price)
	price, err = pm.GetCostOfInboundData("us-central1")
	require.NoError(t, err)
	// Expect inbound data processed price
	require.Equal(t, 0.12, price)
	price, err = pm.GetCostOfOutboundData("us-central1")
	require.NoError(t, err)
	// Expect outbound data processed price
	require.Equal(t, 0.11, price)
}

func TestNewPricingMap_PopulateSuccess(t *testing.T) {
	// Expect service name lookup and pricing retrieval
	frSku := &billingpb.Sku{
		Category:    &billingpb.Category{ResourceGroup: ResourceGroup},
		Description: forwardingRuleDescription + " hourly",
		GeoTaxonomy: &billingpb.GeoTaxonomy{Regions: []string{"us-west1"}},
		PricingInfo: []*billingpb.PricingInfo{{
			PricingExpression: &billingpb.PricingExpression{
				TieredRates: []*billingpb.PricingExpression_TierRate{{
					UnitPrice: &money.Money{Nanos: 10000000}}}}, // 0.01
		}},
	}
	inSku := &billingpb.Sku{
		Category:    &billingpb.Category{ResourceGroup: ResourceGroup},
		Description: inboundDataProcessedDescription + " per GiB",
		GeoTaxonomy: &billingpb.GeoTaxonomy{Regions: []string{"us-west1"}},
		PricingInfo: []*billingpb.PricingInfo{{
			PricingExpression: &billingpb.PricingExpression{TieredRates: []*billingpb.PricingExpression_TierRate{{
				UnitPrice: &money.Money{Nanos: 50000000}}}}, // 0.05
		}},
	}
	outSku := &billingpb.Sku{
		Category:    &billingpb.Category{ResourceGroup: ResourceGroup},
		Description: outboundDataProcessedDescription + " per GiB",
		GeoTaxonomy: &billingpb.GeoTaxonomy{Regions: []string{"us-west1"}},
		PricingInfo: []*billingpb.PricingInfo{{
			PricingExpression: &billingpb.PricingExpression{
				TieredRates: []*billingpb.PricingExpression_TierRate{{
					UnitPrice: &money.Money{Nanos: 60000000}}}}, // 0.06
		}},
	}

	gcpClient := &stubClient{skus: []*billingpb.Sku{frSku, inSku, outSku}}

	pm, err := newPricingMap(testLogger, gcpClient)
	require.NoError(t, err)

	price, err := pm.GetCostOfForwardingRule("us-west1")
	require.NoError(t, err)
	require.Equal(t, 0.01, price)

	price, err = pm.GetCostOfInboundData("us-west1")
	require.NoError(t, err)
	require.Equal(t, 0.05, price)
	price, err = pm.GetCostOfOutboundData("us-west1")
	require.NoError(t, err)
	require.Equal(t, 0.06, price)
}

func TestNewPricingMap_ErrorNoSKUs(t *testing.T) {
	gcpClient := &stubClient{skus: []*billingpb.Sku{}}

	pm, err := newPricingMap(testLogger, gcpClient)
	require.Nil(t, pm)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrNoSKUsFoundForNetworkingService)
}

func TestRegionNotFoundErrors(t *testing.T) {
	pm := &pricingMap{
		pricing: make(map[string]*pricing),
		logger:  testLogger,
	}

	_, err := pm.GetCostOfForwardingRule("unknown")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrRegionNotFound)
	_, err = pm.GetCostOfInboundData("unknown")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrRegionNotFound)
	_, err = pm.GetCostOfOutboundData("unknown")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrRegionNotFound)
}
