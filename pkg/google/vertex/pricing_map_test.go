package vertex

import (
	"testing"

	"cloud.google.com/go/billing/apiv1/billingpb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/type/money"
)

func TestParseSkus_TokenInputSKU(t *testing.T) {
	pm := &PricingMap{}
	err := pm.ParseSkus([]*billingpb.Sku{
		newTokenSKU("Gemini 1.5 Flash Input tokens", "us-central1", "k{char}", 0, 1250000),
	})
	require.NoError(t, err)

	snap := pm.Snapshot()
	require.NotNil(t, snap.tokens["us-central1"])
	require.NotNil(t, snap.tokens["us-central1"]["gemini-1.5-flash"])
	assert.InDelta(t, 0.00125, snap.tokens["us-central1"]["gemini-1.5-flash"].InputPer1kTokens, 1e-9)
}

func TestParseSkus_TokenOutputSKU(t *testing.T) {
	pm := &PricingMap{}
	err := pm.ParseSkus([]*billingpb.Sku{
		newTokenSKU("Gemini 1.5 Flash Output tokens", "us-central1", "k{char}", 0, 5000000),
	})
	require.NoError(t, err)

	snap := pm.Snapshot()
	require.NotNil(t, snap.tokens["us-central1"]["gemini-1.5-flash"])
	assert.InDelta(t, 0.005, snap.tokens["us-central1"]["gemini-1.5-flash"].OutputPer1kTokens, 1e-9)
}

func TestParseSkus_ClaudeTokenSKU(t *testing.T) {
	pm := &PricingMap{}
	err := pm.ParseSkus([]*billingpb.Sku{
		newTokenSKU("Claude 3.5 Sonnet Input tokens", "global", "k{char}", 0, 3000000),
		newTokenSKU("Claude 3.5 Sonnet Output tokens", "global", "k{char}", 0, 15000000),
	})
	require.NoError(t, err)

	snap := pm.Snapshot()
	require.NotNil(t, snap.tokens["global"]["claude-3.5-sonnet"])
	assert.InDelta(t, 0.003, snap.tokens["global"]["claude-3.5-sonnet"].InputPer1kTokens, 1e-9)
	assert.InDelta(t, 0.015, snap.tokens["global"]["claude-3.5-sonnet"].OutputPer1kTokens, 1e-9)
}

func TestParseSkus_TokenSKUNormalizesPerUnitPrice(t *testing.T) {
	// A SKU with no "k" prefix in UsageUnit should be multiplied by 1000.
	pm := &PricingMap{}
	err := pm.ParseSkus([]*billingpb.Sku{
		newTokenSKU("Gemini 1.0 Pro Input tokens", "us-central1", "char", 0, 1250),
	})
	require.NoError(t, err)

	snap := pm.Snapshot()
	assert.InDelta(t, 0.00125, snap.tokens["us-central1"]["gemini-1.0-pro"].InputPer1kTokens, 1e-9)
}

func TestParseSkus_ComputeOnDemand(t *testing.T) {
	pm := &PricingMap{}
	err := pm.ParseSkus([]*billingpb.Sku{
		newComputeSKU("Custom Training n1-standard-4 running in us-central1", "us-central1", 0, 500000000),
	})
	require.NoError(t, err)

	snap := pm.Snapshot()
	require.NotNil(t, snap.compute["us-central1"])
	require.NotNil(t, snap.compute["us-central1"]["n1-standard-4"])
	require.NotNil(t, snap.compute["us-central1"]["n1-standard-4"]["training"])
	assert.InDelta(t, 0.5, snap.compute["us-central1"]["n1-standard-4"]["training"].OnDemandPerHour, 1e-9)
	assert.Equal(t, 0.0, snap.compute["us-central1"]["n1-standard-4"]["training"].SpotPerHour)
}

func TestParseSkus_ComputeSpot(t *testing.T) {
	pm := &PricingMap{}
	err := pm.ParseSkus([]*billingpb.Sku{
		newComputeSKU("Spot Custom Prediction n1-highmem-8 running in europe-west1", "europe-west1", 0, 150000000),
	})
	require.NoError(t, err)

	snap := pm.Snapshot()
	require.NotNil(t, snap.compute["europe-west1"]["n1-highmem-8"]["prediction"])
	assert.InDelta(t, 0.15, snap.compute["europe-west1"]["n1-highmem-8"]["prediction"].SpotPerHour, 1e-9)
	assert.Equal(t, 0.0, snap.compute["europe-west1"]["n1-highmem-8"]["prediction"].OnDemandPerHour)
}

func TestParseSkus_EmbeddingCharactersSKU(t *testing.T) {
	pm := &PricingMap{}
	err := pm.ParseSkus([]*billingpb.Sku{
		newTokenSKU("Gemini Embedding 001 Input characters", "us-central1", "k{char}", 0, 25000),
		newTokenSKU("Gemini Embedding 001 Output characters", "us-central1", "k{char}", 0, 0),
	})
	require.NoError(t, err)

	snap := pm.Snapshot()
	require.NotNil(t, snap.tokens["us-central1"]["gemini-embedding-001"])
	assert.InDelta(t, 0.000025, snap.tokens["us-central1"]["gemini-embedding-001"].InputPer1kTokens, 1e-9)
}

func TestParseSkus_RerankingSKU(t *testing.T) {
	pm := &PricingMap{}
	err := pm.ParseSkus([]*billingpb.Sku{
		// usageUnit "k{request}" is already per-1k, price passes through unchanged.
		newTokenSKU("Semantic Ranker API Ranking Requests", "global", "k{request}", 0, 1000000),
	})
	require.NoError(t, err)

	snap := pm.Snapshot()
	require.NotNil(t, snap.reranking["global"])
	assert.InDelta(t, 0.001, snap.reranking["global"]["semantic-ranker-api"], 1e-9)
}

func TestParseSkus_UnknownSKUsIgnored(t *testing.T) {
	pm := &PricingMap{}
	err := pm.ParseSkus([]*billingpb.Sku{
		newComputeSKU("Some Unknown Vertex AI SKU", "us-central1", 0, 100000000),
		newTokenSKU("Gemini 1.5 Flash Input tokens", "us-central1", "k{char}", 0, 1250000),
	})
	require.NoError(t, err)

	snap := pm.Snapshot()
	assert.Len(t, snap.tokens["us-central1"], 1)
	assert.Empty(t, snap.compute)
}

func TestParseSkus_NilSKUIgnored(t *testing.T) {
	pm := &PricingMap{}
	err := pm.ParseSkus([]*billingpb.Sku{nil})
	require.NoError(t, err)
}

func TestParseSkus_GlobalFallbackForTokenSKUWithNoRegion(t *testing.T) {
	// Gemini token SKUs have no ServiceRegions or GeoTaxonomy; they should be
	// emitted under region="global".
	sku := &billingpb.Sku{
		Description: "Gemini 1.5 Flash Input tokens",
		PricingInfo: []*billingpb.PricingInfo{
			{
				PricingExpression: &billingpb.PricingExpression{
					UsageUnit: "k{char}",
					TieredRates: []*billingpb.PricingExpression_TierRate{
						{UnitPrice: &money.Money{Nanos: 1250000}},
					},
				},
			},
		},
	}

	pm := &PricingMap{}
	err := pm.ParseSkus([]*billingpb.Sku{sku})
	require.NoError(t, err)

	snap := pm.Snapshot()
	require.NotNil(t, snap.tokens["global"])
	require.NotNil(t, snap.tokens["global"]["gemini-1.5-flash"])
	assert.InDelta(t, 0.00125, snap.tokens["global"]["gemini-1.5-flash"].InputPer1kTokens, 1e-9)
}

func TestParseSkus_MultipleRegions(t *testing.T) {
	sku := &billingpb.Sku{
		Description:    "Gemini 1.5 Pro Input tokens",
		ServiceRegions: []string{"us-central1", "europe-west1"},
		PricingInfo: []*billingpb.PricingInfo{
			{
				PricingExpression: &billingpb.PricingExpression{
					UsageUnit: "k{char}",
					TieredRates: []*billingpb.PricingExpression_TierRate{
						{UnitPrice: &money.Money{Nanos: 1250000}},
					},
				},
			},
		},
	}

	pm := &PricingMap{}
	err := pm.ParseSkus([]*billingpb.Sku{sku})
	require.NoError(t, err)

	snap := pm.Snapshot()
	assert.NotNil(t, snap.tokens["us-central1"]["gemini-1.5-pro"])
	assert.NotNil(t, snap.tokens["europe-west1"]["gemini-1.5-pro"])
}

func newTokenSKU(description, region, usageUnit string, units int64, nanos int32) *billingpb.Sku {
	return &billingpb.Sku{
		Description:    description,
		ServiceRegions: []string{region},
		PricingInfo: []*billingpb.PricingInfo{
			{
				PricingExpression: &billingpb.PricingExpression{
					UsageUnit: usageUnit,
					TieredRates: []*billingpb.PricingExpression_TierRate{
						{UnitPrice: &money.Money{Units: units, Nanos: nanos}},
					},
				},
			},
		},
	}
}

func newComputeSKU(description, region string, units int64, nanos int32) *billingpb.Sku {
	return &billingpb.Sku{
		Description:    description,
		ServiceRegions: []string{region},
		PricingInfo: []*billingpb.PricingInfo{
			{
				PricingExpression: &billingpb.PricingExpression{
					TieredRates: []*billingpb.PricingExpression_TierRate{
						{UnitPrice: &money.Money{Units: units, Nanos: nanos}},
					},
				},
			},
		},
	}
}
