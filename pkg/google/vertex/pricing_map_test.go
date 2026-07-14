package vertex

import (
	"regexp"
	"testing"

	"cloud.google.com/go/billing/apiv1/billingpb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/type/money"
)

func TestParseSkus_FamilyFilterDropsUnmatchedFamilies(t *testing.T) {
	pm := &PricingMap{logger: testLogger(), familyFilter: regexp.MustCompile(`^google$`)}
	err := pm.ParseSkus([]*billingpb.Sku{
		newTokenSKU("Gemini 1.5 Flash Input tokens", "us-central1", "k{char}", 0, 1250000),
		newTokenSKU("Llama 4 Maverick Input Tokens", "us-central1", "k{char}", 0, 1250000),
	})
	require.NoError(t, err)

	snap := pm.Snapshot()
	// google family is kept.
	assert.NotNil(t, snap.tokenInput["us-central1"]["gemini-1.5-flash"])
	// meta family (llama) is dropped before entering the map.
	assert.Nil(t, snap.tokenInput["us-central1"]["llama-4-maverick"])
}

func TestParseSkus_TokenInputSKU(t *testing.T) {
	pm := &PricingMap{logger: testLogger()}
	err := pm.ParseSkus([]*billingpb.Sku{
		newTokenSKU("Gemini 1.5 Flash Input tokens", "us-central1", "k{char}", 0, 1250000),
	})
	require.NoError(t, err)

	snap := pm.Snapshot()
	require.NotNil(t, snap.tokenInput["us-central1"])
	assert.InDelta(t, 0.00125, snap.tokenInput["us-central1"]["gemini-1.5-flash"]["on_demand"], 1e-9)
}

func TestParseSkus_TokenOutputSKU(t *testing.T) {
	pm := &PricingMap{logger: testLogger()}
	err := pm.ParseSkus([]*billingpb.Sku{
		newTokenSKU("Gemini 1.5 Flash Output tokens", "us-central1", "k{char}", 0, 5000000),
	})
	require.NoError(t, err)

	snap := pm.Snapshot()
	assert.InDelta(t, 0.005, snap.tokenOutput["us-central1"]["gemini-1.5-flash"]["on_demand"], 1e-9)
}

func TestParseSkus_TokenSKUNormalizesPerUnitPrice(t *testing.T) {
	// A SKU with no "k" prefix in UsageUnit should be multiplied by 1000.
	pm := &PricingMap{logger: testLogger()}
	err := pm.ParseSkus([]*billingpb.Sku{
		newTokenSKU("Gemini 1.0 Pro Input tokens", "us-central1", "char", 0, 1250),
	})
	require.NoError(t, err)

	snap := pm.Snapshot()
	assert.InDelta(t, 0.00125, snap.tokenInput["us-central1"]["gemini-1.0-pro"]["on_demand"], 1e-9)
}

func TestParseSkus_CharacterSKUsRoutedSeparately(t *testing.T) {
	// Character-priced models must land in snap.characters, not snap.tokens.
	pm := &PricingMap{logger: testLogger()}
	err := pm.ParseSkus([]*billingpb.Sku{
		newTokenSKU("Translation LLM Input Characters", "global", "count", 0, 50000),
		newTokenSKU("Translation LLM Output Characters", "global", "count", 0, 150000),
		newTokenSKU("Llama 4 Scout Input Tokens", "global", "count", 0, 170000),
	})
	require.NoError(t, err)

	snap := pm.Snapshot()

	// Character-priced SKUs go to char maps.
	assert.InDelta(t, 0.05, snap.charInput["global"]["translation-llm"]["on_demand"], 1e-9)
	assert.InDelta(t, 0.15, snap.charOutput["global"]["translation-llm"]["on_demand"], 1e-9)

	// Token-priced SKUs still go to token maps.
	assert.InDelta(t, 0.17, snap.tokenInput["global"]["llama-4-scout"]["on_demand"], 1e-9)

	// Character-priced model must not appear in token maps.
	assert.Zero(t, snap.tokenInput["global"]["translation-llm"])
	assert.Zero(t, snap.tokenOutput["global"]["translation-llm"])
}

func TestParseSkus_RerankingSKU(t *testing.T) {
	pm := &PricingMap{logger: testLogger()}
	err := pm.ParseSkus([]*billingpb.Sku{
		// usageUnit "k{request}" is already per-1k, price passes through unchanged.
		newTokenSKU("Semantic Ranker API Ranking Requests", "global", "k{request}", 0, 1000000),
	})
	require.NoError(t, err)

	snap := pm.Snapshot()
	require.NotNil(t, snap.reranking["global"])
	assert.InDelta(t, 0.001, snap.reranking["global"]["semantic-ranker-api"], 1e-9)
}

func TestParseSkus_ModelGardenMaaSPrefixStripped(t *testing.T) {
	// GCP sometimes prefixes Model Garden MaaS output SKUs with a long billing path
	// while the input SKU uses the short name. Both should normalize to the same model ID.
	pm := &PricingMap{logger: testLogger()}
	err := pm.ParseSkus([]*billingpb.Sku{
		newTokenSKU("Llama 4 Maverick Input tokens", "global", "k{char}", 0, 350000),
		newTokenSKU("Cloud Vertex AI Model Garden Model as a Service Llama 4 Maverick Output tokens", "global", "k{char}", 0, 1150000),
	})
	require.NoError(t, err)

	snap := pm.Snapshot()
	assert.InDelta(t, 0.00035, snap.tokenInput["global"]["llama-4-maverick"]["on_demand"], 1e-9)
	assert.InDelta(t, 0.00115, snap.tokenOutput["global"]["llama-4-maverick"]["on_demand"], 1e-9)
	// The long-prefix key must not exist as a separate entry.
	assert.Zero(t, snap.tokenInput["global"]["cloud-vertex-ai-model-garden-model-as-a-service-llama-4-maverick"])
}

func TestParseSkus_UnknownSKUsIgnored(t *testing.T) {
	pm := &PricingMap{logger: testLogger()}
	err := pm.ParseSkus([]*billingpb.Sku{
		newTokenSKU("Some Unknown Vertex AI SKU", "us-central1", "count", 0, 100000000),
		newTokenSKU("Gemini 1.5 Flash Input tokens", "us-central1", "k{char}", 0, 1250000),
	})
	require.NoError(t, err)

	snap := pm.Snapshot()
	assert.Len(t, snap.tokenInput["us-central1"], 1)
}

func TestParseSkus_NilSKUIgnored(t *testing.T) {
	pm := &PricingMap{logger: testLogger()}
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

	pm := &PricingMap{logger: testLogger()}
	err := pm.ParseSkus([]*billingpb.Sku{sku})
	require.NoError(t, err)

	snap := pm.Snapshot()
	require.NotNil(t, snap.tokenInput["global"])
	assert.InDelta(t, 0.00125, snap.tokenInput["global"]["gemini-1.5-flash"]["on_demand"], 1e-9)
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

	pm := &PricingMap{logger: testLogger()}
	err := pm.ParseSkus([]*billingpb.Sku{sku})
	require.NoError(t, err)

	snap := pm.Snapshot()
	assert.NotZero(t, snap.tokenInput["us-central1"]["gemini-1.5-pro"]["on_demand"])
	assert.NotZero(t, snap.tokenInput["europe-west1"]["gemini-1.5-pro"]["on_demand"])
}

func TestParseSkus_GeminiBatchInputSKU(t *testing.T) {
	pm := &PricingMap{logger: testLogger()}
	err := pm.ParseSkus([]*billingpb.Sku{
		newTokenSKU("Gemini 2.5 Flash Text Input - Batch Predictions", "us-central1", "k{char}", 0, 75000),
	})
	require.NoError(t, err)

	snap := pm.Snapshot()
	assert.InDelta(t, 0.000075, snap.tokenInput["us-central1"]["gemini-2.5-flash"]["batch"], 1e-9)
}

func TestParseSkus_GeminiOnDemandInputSKU(t *testing.T) {
	pm := &PricingMap{logger: testLogger()}
	err := pm.ParseSkus([]*billingpb.Sku{
		newTokenSKU("Gemini 2.5 Flash Text Input - Predictions", "us-central1", "k{char}", 0, 150000),
	})
	require.NoError(t, err)

	snap := pm.Snapshot()
	assert.NotZero(t, snap.tokenInput["us-central1"]["gemini-2.5-flash"]["on_demand"])
}

func TestParseSkus_GeminiThinkingOutputSKU(t *testing.T) {
	pm := &PricingMap{logger: testLogger()}
	err := pm.ParseSkus([]*billingpb.Sku{
		newTokenSKU("Gemini 2.5 Flash Thinking Text Output - Predictions", "global", "k{char}", 0, 350000),
	})
	require.NoError(t, err)

	snap := pm.Snapshot()
	assert.NotZero(t, snap.tokenOutput["global"]["gemini-2.5-flash"]["thinking"])
}

func TestParseSkus_GeminiCachedInputSKU(t *testing.T) {
	pm := &PricingMap{logger: testLogger()}
	err := pm.ParseSkus([]*billingpb.Sku{
		newTokenSKU("Gemini 2.0 Flash Input Text Caching", "global", "k{char}", 0, 25000),
	})
	require.NoError(t, err)

	snap := pm.Snapshot()
	assert.NotZero(t, snap.tokenInput["global"]["gemini-2.0-flash"]["cached"])
}

func TestParseSkus_GeminiLiveInputSKU(t *testing.T) {
	pm := &PricingMap{logger: testLogger()}
	err := pm.ParseSkus([]*billingpb.Sku{
		newTokenSKU("Gemini 2.5 Flash Live Text Input - Predictions", "global", "k{char}", 0, 100000),
	})
	require.NoError(t, err)

	snap := pm.Snapshot()
	assert.NotZero(t, snap.tokenInput["global"]["gemini-2.5-flash"]["live"])
}

func TestParseSkus_MaaSBatchSKU(t *testing.T) {
	pm := &PricingMap{logger: testLogger()}
	err := pm.ParseSkus([]*billingpb.Sku{
		newTokenSKU("Cloud Vertex AI Model Garden Model as a Service Llama 4 Maverick Batch Input Token", "global", "k{char}", 0, 200000),
	})
	require.NoError(t, err)

	snap := pm.Snapshot()
	assert.NotZero(t, snap.tokenInput["global"]["llama-4-maverick"]["batch"])
}

func TestParseSkus_MaaSCachedSKU(t *testing.T) {
	pm := &PricingMap{logger: testLogger()}
	err := pm.ParseSkus([]*billingpb.Sku{
		newTokenSKU("Cloud Vertex AI Model Garden Model as a Service DeepSeek-V3.1 Cached Text Input Token", "global", "k{char}", 0, 100000),
	})
	require.NoError(t, err)

	snap := pm.Snapshot()
	assert.NotZero(t, snap.tokenInput["global"]["deepseek-v3.1"]["cached"])
}

func TestParseSkus_MaaSOnDemandSKU(t *testing.T) {
	pm := &PricingMap{logger: testLogger()}
	err := pm.ParseSkus([]*billingpb.Sku{
		newTokenSKU("Cloud Vertex AI Model Garden Model as a Service Llama 4 Scout Input Tokens", "global", "k{char}", 0, 170000),
	})
	require.NoError(t, err)

	snap := pm.Snapshot()
	assert.NotZero(t, snap.tokenInput["global"]["llama-4-scout"]["on_demand"])
}

func TestRegexPatterns(t *testing.T) {
	t.Run("rerankRegex", func(t *testing.T) {
		cases := []struct {
			input string
			match bool
		}{
			{"Semantic Ranker API Ranking Requests", true},
			{"Semantic Ranker API Ranking Request", true}, // singular
			{"Some Model Ranking requests", true},         // case insensitive
			{"Gemini 1.5 Flash Input tokens", false},
			{"Ranking Requests", false}, // no model prefix
		}
		for _, tc := range cases {
			assert.Equal(t, tc.match, rerankRegex.MatchString(tc.input), "input: %q", tc.input)
		}
	})
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
