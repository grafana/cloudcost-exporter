package bedrock

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	mockclient "github.com/grafana/cloudcost-exporter/pkg/aws/client/mocks"
	"github.com/grafana/cloudcost-exporter/pkg/utils"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestNew_Succeeds(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	pricingClient := mockclient.NewMockClient(ctrl)
	pricingClient.EXPECT().
		ListBedrockPrices(gomock.Any(), "us-east-1").
		Return([]string{inputPriceJSON("us-east-1", "USE1", "Claude3Sonnet", "Anthropic", "0.00300")}, nil).
		Times(1)
	pricingClient.EXPECT().
		ListBedrockMarketplacePrices(gomock.Any(), "us-east-1").
		Return([]string{}, nil).
		Times(1)

	collector, err := New(t.Context(), &Config{
		Regions:       []ec2types.Region{{RegionName: aws.String("us-east-1")}},
		PricingClient: pricingClient,
		AccountID:     "123456789012",
	}, testLogger())

	require.NoError(t, err)
	assert.NotNil(t, collector)
}

func TestNew_ReturnsErrorWhenPricingAPIUnavailable(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	pricingClient := mockclient.NewMockClient(ctrl)
	pricingClient.EXPECT().
		ListBedrockPrices(gomock.Any(), "us-east-1").
		Return(nil, fmt.Errorf("pricing API unavailable")).
		Times(1)

	collector, err := New(t.Context(), &Config{
		Regions:       []ec2types.Region{{RegionName: aws.String("us-east-1")}},
		PricingClient: pricingClient,
	}, testLogger())

	assert.Nil(t, collector)
	require.Error(t, err)
}

func TestCollect_EmitsInputAndOutputTokenMetrics(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	pricingClient := mockclient.NewMockClient(ctrl)
	pricingClient.EXPECT().
		ListBedrockPrices(gomock.Any(), "us-east-1").
		Return([]string{
			inputPriceJSON("us-east-1", "USE1", "Claude3Sonnet", "Anthropic", "0.00300"),
			outputPriceJSON("us-east-1", "USE1", "Claude3Sonnet", "Anthropic", "0.01500"),
		}, nil).
		Times(1)
	pricingClient.EXPECT().
		ListBedrockMarketplacePrices(gomock.Any(), "us-east-1").
		Return([]string{}, nil).
		Times(1)

	collector, err := New(t.Context(), &Config{
		Regions:       []ec2types.Region{{RegionName: aws.String("us-east-1")}},
		PricingClient: pricingClient,
		AccountID:     "123456789012",
	}, testLogger())
	require.NoError(t, err)

	results, err := collectMetricResults(t, collector)
	require.NoError(t, err)
	require.Len(t, results, 2)

	inputMetric := tokenMetricByType(results, "input")
	require.NotNil(t, inputMetric)
	assert.Equal(t, "us-east-1", inputMetric.Labels["region"])
	// These fixtures omit the `model` attribute, so model_id is the normalized usagetype slug
	// (fallback path). Real Claude SKUs carry `model`, yielding claude-3-sonnet; see
	// TestCollect_StandardModelIDUsesModelAttribute.
	assert.Equal(t, "claude3sonnet", inputMetric.Labels["model_id"])
	assert.Equal(t, "anthropic", inputMetric.Labels["family"])
	assert.Equal(t, "in", inputMetric.Labels["region_tier"])
	assert.Equal(t, "standard", inputMetric.Labels["quota_tier"])
	assert.Equal(t, "", inputMetric.Labels["cache_ttl"])
	assert.Equal(t, "123456789012", inputMetric.Labels["account_id"])
	assert.InDelta(t, 0.003, inputMetric.Value, 1e-9)

	outputMetric := tokenMetricByType(results, "output")
	require.NotNil(t, outputMetric)
	assert.Equal(t, "us-east-1", outputMetric.Labels["region"])
	assert.Equal(t, "claude3sonnet", outputMetric.Labels["model_id"])
	assert.Equal(t, "anthropic", outputMetric.Labels["family"])
	assert.Equal(t, "in", outputMetric.Labels["region_tier"])
	assert.Equal(t, "standard", outputMetric.Labels["quota_tier"])
	assert.InDelta(t, 0.015, outputMetric.Value, 1e-9)
}

func TestCollect_EmitsMetricsForMultipleModels(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	pricingClient := mockclient.NewMockClient(ctrl)
	pricingClient.EXPECT().
		ListBedrockPrices(gomock.Any(), "us-east-1").
		Return([]string{
			inputPriceJSON("us-east-1", "USE1", "Claude3Sonnet", "Anthropic", "0.00300"),
			outputPriceJSON("us-east-1", "USE1", "Claude3Sonnet", "Anthropic", "0.01500"),
			inputPriceJSON("us-east-1", "USE1", "NovaPro", "", "0.00080"),
			outputPriceJSON("us-east-1", "USE1", "NovaPro", "", "0.00320"),
		}, nil).
		Times(1)
	pricingClient.EXPECT().
		ListBedrockMarketplacePrices(gomock.Any(), "us-east-1").
		Return([]string{}, nil).
		Times(1)

	collector, err := New(t.Context(), &Config{
		Regions:       []ec2types.Region{{RegionName: aws.String("us-east-1")}},
		PricingClient: pricingClient,
		AccountID:     "123456789012",
	}, testLogger())
	require.NoError(t, err)

	results, err := collectMetricResults(t, collector)
	require.NoError(t, err)
	assert.Len(t, results, 4)
}

func TestCollect_LabelsCrossRegionPriceTier(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	pricingClient := mockclient.NewMockClient(ctrl)
	pricingClient.EXPECT().
		ListBedrockPrices(gomock.Any(), "us-east-1").
		Return([]string{
			crossRegionInputPriceJSON("us-east-1", "USE1", "NovaPremier", "", "0.00600"),
		}, nil).
		Times(1)
	pricingClient.EXPECT().
		ListBedrockMarketplacePrices(gomock.Any(), "us-east-1").
		Return([]string{}, nil).
		Times(1)

	collector, err := New(t.Context(), &Config{
		Regions:       []ec2types.Region{{RegionName: aws.String("us-east-1")}},
		PricingClient: pricingClient,
		AccountID:     "123456789012",
	}, testLogger())
	require.NoError(t, err)

	results, err := collectMetricResults(t, collector)
	require.NoError(t, err)
	require.Len(t, results, 1)

	m := results[0]
	assert.Equal(t, "cross", m.Labels["region_tier"])
	assert.Equal(t, "standard", m.Labels["quota_tier"])
	assert.Equal(t, "input", m.Labels["token_type"])
	assert.Equal(t, "amazon", m.Labels["family"])
	assert.Equal(t, "novapremier", m.Labels["model_id"])
}

func TestCollect_LabelsBatchPriceTier(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	pricingClient := mockclient.NewMockClient(ctrl)
	pricingClient.EXPECT().
		ListBedrockPrices(gomock.Any(), "us-east-1").
		Return([]string{
			batchInputPriceJSON("us-east-1", "USE1", "Claude3Sonnet", "Anthropic", "0.00150"),
		}, nil).
		Times(1)
	pricingClient.EXPECT().
		ListBedrockMarketplacePrices(gomock.Any(), "us-east-1").
		Return([]string{}, nil).
		Times(1)

	collector, err := New(t.Context(), &Config{
		Regions:       []ec2types.Region{{RegionName: aws.String("us-east-1")}},
		PricingClient: pricingClient,
		AccountID:     "123456789012",
	}, testLogger())
	require.NoError(t, err)

	results, err := collectMetricResults(t, collector)
	require.NoError(t, err)
	require.Len(t, results, 1)

	m := results[0]
	assert.Equal(t, "in", m.Labels["region_tier"])
	assert.Equal(t, "batch", m.Labels["quota_tier"])
	assert.Equal(t, "input", m.Labels["token_type"])
	assert.Equal(t, "anthropic", m.Labels["family"])
	assert.Equal(t, "claude3sonnet", m.Labels["model_id"])
}

func TestCollect_FamilyFilterRegexFiltersOtherFamilies(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	pricingClient := mockclient.NewMockClient(ctrl)
	pricingClient.EXPECT().
		ListBedrockPrices(gomock.Any(), "us-east-1").
		Return([]string{
			inputPriceJSON("us-east-1", "USE1", "Claude3Sonnet", "Anthropic", "0.00300"),
			inputPriceJSON("us-east-1", "USE1", "Llama4-Scout-17B", "Meta", "0.00017"),
			searchUnitPriceJSON("us-east-1", "USE1", "cohere.rerank-english-v3", "Cohere", "0.00200"),
		}, nil).
		Times(1)
	pricingClient.EXPECT().
		ListBedrockMarketplacePrices(gomock.Any(), "us-east-1").
		Return([]string{}, nil).
		Times(1)

	collector, err := New(t.Context(), &Config{
		Regions:       []ec2types.Region{{RegionName: aws.String("us-east-1")}},
		PricingClient: pricingClient,
		FamilyFilter:  "anthropic|amazon",
		AccountID:     "123456789012",
	}, testLogger())
	require.NoError(t, err)

	results, err := collectMetricResults(t, collector)
	require.NoError(t, err)
	require.Len(t, results, 1)

	m := results[0]
	assert.Equal(t, "anthropic", m.Labels["family"])
	assert.Equal(t, "claude3sonnet", m.Labels["model_id"])
}

func TestCollect_FamilyFilterDefaultEmitsAllFamilies(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	pricingClient := mockclient.NewMockClient(ctrl)
	pricingClient.EXPECT().
		ListBedrockPrices(gomock.Any(), "us-east-1").
		Return([]string{
			inputPriceJSON("us-east-1", "USE1", "Claude3Sonnet", "Anthropic", "0.00300"),
			inputPriceJSON("us-east-1", "USE1", "Llama4-Scout-17B", "Meta", "0.00017"),
			inputPriceJSON("us-east-1", "USE1", "NovaPro", "", "0.00080"),
		}, nil).
		Times(1)
	pricingClient.EXPECT().
		ListBedrockMarketplacePrices(gomock.Any(), "us-east-1").
		Return([]string{}, nil).
		Times(1)

	collector, err := New(t.Context(), &Config{
		Regions:       []ec2types.Region{{RegionName: aws.String("us-east-1")}},
		PricingClient: pricingClient,
		AccountID:     "123456789012",
	}, testLogger())
	require.NoError(t, err)

	results, err := collectMetricResults(t, collector)
	require.NoError(t, err)
	require.Len(t, results, 3)
}

func TestNew_ReturnsErrorForInvalidFamilyFilterRegex(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	pricingClient := mockclient.NewMockClient(ctrl)

	_, err := New(t.Context(), &Config{
		Regions:       []ec2types.Region{{RegionName: aws.String("us-east-1")}},
		PricingClient: pricingClient,
		FamilyFilter:  "[invalid",
	}, testLogger())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid bedrock family filter")
}

func TestCollect_SkipsNonTextTokenSKUs(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	pricingClient := mockclient.NewMockClient(ctrl)
	pricingClient.EXPECT().
		ListBedrockPrices(gomock.Any(), "us-east-1").
		Return([]string{
			inputPriceJSON("us-east-1", "USE1", "Claude3Sonnet", "Anthropic", "0.00300"),
			// Image token entry — should be skipped.
			rawPriceJSON("us-east-1", "USE1-Nova2.0Pro-input-image-token-count-cross-region-global", "Input Image Token Count", "", "0.00125"),
			// Cache entry — should be skipped.
			rawPriceJSON("us-east-1", "USE1-NovaPro-cache-write-input-token-count-custom-model", "Prompt cache write input tokens", "", "0.00000"),
			// Guardrail entry — should be skipped.
			rawPriceJSON("us-east-1", "USE1-Guardrail-AutomatedReasoningPolicyUnitsConsumed", "", "", "0.00017"),
		}, nil).
		Times(1)
	pricingClient.EXPECT().
		ListBedrockMarketplacePrices(gomock.Any(), "us-east-1").
		Return([]string{}, nil).
		Times(1)

	collector, err := New(t.Context(), &Config{
		Regions:       []ec2types.Region{{RegionName: aws.String("us-east-1")}},
		PricingClient: pricingClient,
		AccountID:     "123456789012",
	}, testLogger())
	require.NoError(t, err)

	results, err := collectMetricResults(t, collector)
	require.NoError(t, err)
	assert.Len(t, results, 1)
}

func TestCollect_ReturnsContextErrWhenContextCancelled(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	pricingClient := mockclient.NewMockClient(ctrl)
	pricingClient.EXPECT().
		ListBedrockPrices(gomock.Any(), "us-east-1").
		Return([]string{}, nil).
		Times(1)
	pricingClient.EXPECT().
		ListBedrockMarketplacePrices(gomock.Any(), "us-east-1").
		Return([]string{}, nil).
		Times(1)

	collector, err := New(t.Context(), &Config{
		Regions:       []ec2types.Region{{RegionName: aws.String("us-east-1")}},
		PricingClient: pricingClient,
		AccountID:     "123456789012",
	}, testLogger())
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ch := make(chan prometheus.Metric, 10)
	err = collector.Collect(ctx, ch)
	close(ch)

	assert.ErrorIs(t, err, context.Canceled)
}

func TestClassifyInferenceType(t *testing.T) {
	tests := []struct {
		name          string
		inferenceType string
		wantDirection string
		wantOK        bool
	}{
		{"standard input", "Input tokens", "input", true},
		{"input priority", "Input tokens priority", "input", true},
		{"input flex", "Input tokens flex", "input", true},
		{"input batch lowercase", "input tokens batch", "input", true},
		{"text input token", "Text Input Token", "input", true},
		{"standard output", "Output tokens", "output", true},
		{"output batch", "output tokens batch", "output", true},
		{"output flex", "Output tokens flex", "output", true},
		{"output priority", "Output tokens priority", "output", true},
		{"search units", "Search units", "search", true},
		{"rerank", "Rerank units", "search", true},
		{"image input — skip", "Input Image Token Count", "", false},
		{"video input — skip", "Input Video Token Count Flex", "", false},
		{"audio input — skip", "Input Audio Token Count Flex", "", false},
		{"image output — skip", "Output Image Token Count", "", false},
		{"cache write — skip", "Prompt cache write input tokens", "", false},
		{"cache read — skip", "Prompt cache read input tokens", "", false},
		{"guardrail — skip", "", "", false},
		{"custom image — skip", "Custom T2I 1024 Standard", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			direction, ok := classifyInferenceType(tt.inferenceType)
			assert.Equal(t, tt.wantOK, ok)
			if tt.wantOK {
				assert.Equal(t, tt.wantDirection, direction)
			}
		})
	}
}

func TestParseBedrockModelID(t *testing.T) {
	// Asserts the model slug plus the region/quota tiers decoded from the trailing suffix.
	// Cross-region and a quota qualifier are captured independently (e.g. cross + batch).
	tests := []struct {
		usagetype      string
		wantModelID    string
		wantRegionTier string
		wantQuotaTier  string
	}{
		{"USE1-Claude3Sonnet-input-tokens", "Claude3Sonnet", "in", "standard"},
		{"USE1-Claude2.0-input-tokens", "Claude2.0", "in", "standard"},
		{"USE1-Llama4-Maverick-17B-input-tokens-batch", "Llama4-Maverick-17B", "in", "batch"},
		{"USE1-GPT-OSS-Safeguard-20B-input-tokens-priority", "GPT-OSS-Safeguard-20B", "in", "priority"},
		{"USE1-Gemma-3-4B-IT-input-tokens-flex", "Gemma-3-4B-IT", "in", "flex"},
		{"USE1-Claude3-5Haiku-input-tokens-latency-optimized", "Claude3-5Haiku", "in", "latency_optimized"},
		{"USE1-MistralSmall-input-tokens-batch", "MistralSmall", "in", "batch"},
		{"USE1-Nova2.0Lite-input-tokens-cross-region-global-batch", "Nova2.0Lite", "cross", "batch"},
		{"USE1-Nova2.0Pro-text-input-tokens-priority-cross-region-global", "Nova2.0Pro", "cross", "priority"},
		{"USE1-GPT-OSS-Safeguard-120B-output-tokens-batch", "GPT-OSS-Safeguard-120B", "in", "batch"},
		{"USE1-Llama3-2-1B-output-tokens", "Llama3-2-1B", "in", "standard"},
		{"USE1-cohere.rerank-english-v3-search-units", "cohere.rerank-english-v3", "in", "standard"},
		// Unrecognized format returns empty model ID.
		{"USE1-Guardrail-AutomatedReasoningPolicyUnitsConsumed", "", "in", "standard"},
	}

	for _, tt := range tests {
		t.Run(tt.usagetype, func(t *testing.T) {
			modelID, suffix := parseBedrockModelID(tt.usagetype)
			assert.Equal(t, tt.wantModelID, modelID)
			regionTier, quotaTier := standardTier(suffix)
			assert.Equal(t, tt.wantRegionTier, regionTier)
			assert.Equal(t, tt.wantQuotaTier, quotaTier)
		})
	}
}

func TestStripRedundantLatency(t *testing.T) {
	tests := []struct {
		modelID   string
		quotaTier string
		want      string
	}{
		// Stripped only when quota_tier already records latency-optimized.
		{"nova-pro-latency-optimized", "latency_optimized", "nova-pro"},
		{"llama-3.1-405b-latency-optimized", "latency_optimized", "llama-3.1-405b"},
		// Already clean: no-op.
		{"claude-3.5-haiku", "latency_optimized", "claude-3.5-haiku"},
		// Not latency-optimized: left untouched even if the name somehow contains the token.
		{"nova-pro-latency-optimized", "standard", "nova-pro-latency-optimized"},
		{"nova-pro", "standard", "nova-pro"},
	}
	for _, tt := range tests {
		t.Run(tt.modelID+"/"+tt.quotaTier, func(t *testing.T) {
			assert.Equal(t, tt.want, stripRedundantLatency(tt.modelID, tt.quotaTier))
		})
	}
}

func TestNormalizeProvider(t *testing.T) {
	tests := []struct {
		provider string
		want     string
	}{
		{"Anthropic", "anthropic"},
		{"Meta", "meta"},
		{"OpenAI", "openai"},
		{"Google", "google"},
		{"Mistral", "mistral"},
		{"Nvidia", "nvidia"},
		{"Cohere", "cohere"},
		{"Mistral AI", "mistral_ai"},
		{"Moonshot AI", "moonshot_ai"},
		{"", "amazon"},
	}

	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			assert.Equal(t, tt.want, normalizeProvider(tt.provider))
		})
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// rawPriceJSON builds a minimal Pricing API JSON item with explicit inferenceType and provider,
// and no `model` attribute. SKUs built with it exercise the usagetype-slug fallback in
// encodeBedrockPriceJSON (the path for the rare SKU AWS publishes without a `model` attribute).
func rawPriceJSON(regionCode, usagetype, inferenceType, provider, price string) string {
	return fmt.Sprintf(
		`{"product":{"attributes":{"usagetype":%q,"regionCode":%q,"inferenceType":%q,"provider":%q}},"terms":{"OnDemand":{"term1":{"priceDimensions":{"dim1":{"pricePerUnit":{"USD":%q}}}}}}}`,
		usagetype, regionCode, inferenceType, provider, price,
	)
}

// modelPriceJSON builds a standard SKU that includes the human-readable `model` attribute, which
// is what the live AWS Pricing API returns. model_id is derived from `model`, not the usagetype.
func modelPriceJSON(regionCode, usagetype, inferenceType, provider, model, price string) string {
	return fmt.Sprintf(
		`{"product":{"attributes":{"usagetype":%q,"regionCode":%q,"inferenceType":%q,"provider":%q,"model":%q}},"terms":{"OnDemand":{"term1":{"priceDimensions":{"dim1":{"pricePerUnit":{"USD":%q}}}}}}}`,
		usagetype, regionCode, inferenceType, provider, model, price,
	)
}

func inputPriceJSON(region, regionPrefix, modelSlug, provider, price string) string {
	usagetype := fmt.Sprintf("%s-%s-input-tokens", regionPrefix, modelSlug)
	return rawPriceJSON(region, usagetype, "Input tokens", provider, price)
}

func outputPriceJSON(region, regionPrefix, modelSlug, provider, price string) string {
	usagetype := fmt.Sprintf("%s-%s-output-tokens", regionPrefix, modelSlug)
	return rawPriceJSON(region, usagetype, "Output tokens", provider, price)
}

func batchInputPriceJSON(region, regionPrefix, modelSlug, provider, price string) string {
	usagetype := fmt.Sprintf("%s-%s-input-tokens-batch", regionPrefix, modelSlug)
	return rawPriceJSON(region, usagetype, "input tokens batch", provider, price)
}

func crossRegionInputPriceJSON(region, regionPrefix, modelSlug, provider, price string) string {
	usagetype := fmt.Sprintf("%s-%s-input-tokens-cross-region-global", regionPrefix, modelSlug)
	return rawPriceJSON(region, usagetype, "Input tokens", provider, price)
}

func searchUnitPriceJSON(region, regionPrefix, modelSlug, provider, price string) string {
	usagetype := fmt.Sprintf("%s-%s-search-units", regionPrefix, modelSlug)
	return rawPriceJSON(region, usagetype, "Search units", provider, price)
}

func collectMetricResults(t *testing.T, collector *Collector) ([]*utils.MetricResult, error) {
	t.Helper()

	ch := make(chan prometheus.Metric, 20)
	err := collector.Collect(t.Context(), ch)
	close(ch)

	var results []*utils.MetricResult
	for metric := range ch {
		results = append(results, utils.ReadMetrics(metric))
	}

	return results, err
}

// tokenMetricByType returns the token-cost metric for a given token_type (input, output,
// cache_read, cache_write). Input and output now share one metric name, distinguished by label.
func tokenMetricByType(results []*utils.MetricResult, tokenType string) *utils.MetricResult {
	for _, result := range results {
		if result.FqName == "cloudcost_aws_bedrock_usd_per_1k_tokens" && result.Labels["token_type"] == tokenType {
			return result
		}
	}
	return nil
}

// marketplacePriceJSON builds a minimal AmazonBedrockFoundationModels SKU JSON item.
func marketplacePriceJSON(servicename, usagetype, regionCode, price string) string {
	return fmt.Sprintf(
		`{"product":{"attributes":{"usagetype":%q,"regionCode":%q,"servicename":%q}},"terms":{"OnDemand":{"term1":{"priceDimensions":{"dim1":{"pricePerUnit":{"USD":%q}}}}}}}`,
		usagetype, regionCode, servicename, price,
	)
}

func TestCollect_MergesLegacyClaudeAcrossSources(t *testing.T) {
	// The standard source prices legacy Claude with input tokens only; the marketplace source
	// adds the output price under the same model_id. Both must be emitted: the overlapping input
	// key dedups in the pricing store (equal prices), and the marketplace output fills the gap.
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	pricingClient := mockclient.NewMockClient(ctrl)
	pricingClient.EXPECT().
		ListBedrockPrices(gomock.Any(), "us-east-1").
		Return([]string{
			modelPriceJSON("us-east-1", "USE1-Claude3Sonnet-input-tokens", "Input tokens", "Anthropic", "Claude 3 Sonnet", "0.00300"),
		}, nil).
		Times(1)
	pricingClient.EXPECT().
		ListBedrockMarketplacePrices(gomock.Any(), "us-east-1").
		Return([]string{
			// Same model_id (claude-3-sonnet) as standard: input matches, output is new.
			marketplacePriceJSON("Claude 3 Sonnet (Amazon Bedrock Edition)", "USE1-MP:USE1_InputTokenCount-Units", "us-east-1", "3.0"),
			marketplacePriceJSON("Claude 3 Sonnet (Amazon Bedrock Edition)", "USE1-MP:USE1_OutputTokenCount-Units", "us-east-1", "15.0"),
		}, nil).
		Times(1)

	collector, err := New(t.Context(), &Config{
		Regions:       []ec2types.Region{{RegionName: aws.String("us-east-1")}},
		PricingClient: pricingClient,
		AccountID:     "123456789012",
	}, testLogger())
	require.NoError(t, err)

	results, err := collectMetricResults(t, collector)
	require.NoError(t, err)

	var input, output *utils.MetricResult
	for _, r := range results {
		if r.FqName != "cloudcost_aws_bedrock_usd_per_1k_tokens" || r.Labels["model_id"] != "claude-3-sonnet" {
			continue
		}
		switch r.Labels["token_type"] {
		case "input":
			input = r
		case "output":
			output = r
		}
	}
	// Input appears once (deduped across sources), output comes from the marketplace source.
	require.NotNil(t, input, "claude-3-sonnet input should be emitted")
	require.NotNil(t, output, "claude-3-sonnet output should be recovered from marketplace")
	assert.InDelta(t, 0.003, input.Value, 1e-9)
	assert.InDelta(t, 0.015, output.Value, 1e-9)
}

func TestNormalizeModelID(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{"Claude Sonnet 4.6", "claude-sonnet-4.6"},
		{"Cohere Rerank v3.5", "cohere-rerank-v3.5"},
		// " - " separators collapse to a single hyphen instead of "---".
		{"Cohere Embed 3 Model - English", "cohere-embed-3-model-english"},
		{"Cohere Generate Model - Command Light", "cohere-generate-model-command-light"},
		// Parenthesis content is kept (sans parens) so context variants stay distinct.
		{"Claude (100K)", "claude-100k"},
		{"Claude Instant (100K)", "claude-instant-100k"},
		{"Claude", "claude"},
		{"  Padded Name  ", "padded-name"},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			assert.Equal(t, tt.want, normalizeModelID(tt.raw))
		})
	}
}

func TestCollect_StandardModelIDUsesModelAttribute(t *testing.T) {
	// The standard source derives model_id from the human-readable `model` attribute, normalized
	// to the same lowercase-hyphen slug the marketplace source uses, so the two match.
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	pricingClient := mockclient.NewMockClient(ctrl)
	pricingClient.EXPECT().
		ListBedrockPrices(gomock.Any(), "us-east-1").
		Return([]string{
			modelPriceJSON("us-east-1", "USE1-Claude3Sonnet-input-tokens", "Input tokens", "Anthropic", "Claude 3 Sonnet", "0.00300"),
			modelPriceJSON("us-east-1", "USE1-Llama3-1-405B-input-tokens", "Input tokens", "Meta", "Llama 3.1 405B", "0.00240"),
		}, nil).
		Times(1)
	pricingClient.EXPECT().
		ListBedrockMarketplacePrices(gomock.Any(), "us-east-1").
		Return([]string{}, nil).
		Times(1)

	collector, err := New(t.Context(), &Config{
		Regions:       []ec2types.Region{{RegionName: aws.String("us-east-1")}},
		PricingClient: pricingClient,
		AccountID:     "123456789012",
	}, testLogger())
	require.NoError(t, err)

	results, err := collectMetricResults(t, collector)
	require.NoError(t, err)

	ids := map[string]bool{}
	for _, r := range results {
		ids[r.Labels["model_id"]] = true
	}
	assert.True(t, ids["claude-3-sonnet"], "expected normalized claude-3-sonnet, got %v", ids)
	assert.True(t, ids["llama-3.1-405b"], "expected normalized llama-3.1-405b, got %v", ids)
}

func TestCollect_StandardModelIDDisambiguatesNovaSonicModality(t *testing.T) {
	// Nova Sonic prices text and speech tokens differently while publishing one `model` name.
	// The modality must fold into model_id so the two prices do not collide on a single key.
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	pricingClient := mockclient.NewMockClient(ctrl)
	pricingClient.EXPECT().
		ListBedrockPrices(gomock.Any(), "us-east-1").
		Return([]string{
			modelPriceJSON("us-east-1", "USE1-NovaSonic-text-input-tokens", "Input tokens", "", "Nova Sonic", "0.00006"),
			modelPriceJSON("us-east-1", "USE1-NovaSonic-speech-input-tokens", "Input tokens", "", "Nova Sonic", "0.00340"),
		}, nil).
		Times(1)
	pricingClient.EXPECT().
		ListBedrockMarketplacePrices(gomock.Any(), "us-east-1").
		Return([]string{}, nil).
		Times(1)

	collector, err := New(t.Context(), &Config{
		Regions:       []ec2types.Region{{RegionName: aws.String("us-east-1")}},
		PricingClient: pricingClient,
		AccountID:     "123456789012",
	}, testLogger())
	require.NoError(t, err)

	results, err := collectMetricResults(t, collector)
	require.NoError(t, err)
	require.Len(t, results, 2, "text and speech must not collide into one series")

	byID := map[string]float64{}
	for _, r := range results {
		byID[r.Labels["model_id"]] = r.Value
	}
	assert.InDelta(t, 0.00006, byID["nova-sonic-text"], 1e-9)
	assert.InDelta(t, 0.00340, byID["nova-sonic-speech"], 1e-9)
}

func TestFamilyFromServiceName(t *testing.T) {
	tests := []struct {
		servicename string
		want        string
	}{
		{"Claude Sonnet 4.6 (Amazon Bedrock Edition)", "anthropic"},
		{"Claude Opus 4 (Amazon Bedrock Edition)", "anthropic"},
		{"Cohere Rerank v3.5 (Amazon Bedrock Edition)", "cohere"},
		{"Cohere Embed 4 Model (Amazon Bedrock Edition)", "cohere"},
		{"Meta Llama 2 Chat 70B (Amazon Bedrock Edition)", "meta"},
		{"Jamba 1.5 Large (Amazon Bedrock Edition)", "ai21"},
		{"Jurassic-2 Ultra (Amazon Bedrock Edition)", "ai21"},
		{"Stable Diffusion 3 Large v1.0 (Amazon Bedrock Edition)", "stability"},
		{"Palmyra X5 (Amazon Bedrock Edition)", "writer"},
		{"TwelveLabs Marengo Embed 3.0 (Amazon Bedrock Edition)", "twelvelabs"},
		{"Unknown Model (Amazon Bedrock Edition)", "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.servicename, func(t *testing.T) {
			assert.Equal(t, tt.want, familyFromServiceName(tt.servicename))
		})
	}
}

func TestClassifyMarketplaceUsageType(t *testing.T) {
	tests := []struct {
		usagetype     string
		wantDirection string
		wantOK        bool
	}{
		{"USE1-MP:USE1_InputTokenCount-Units", "input", true},
		{"USE1-MP:USE1_InputTokenCount_Global-Units", "input", true},
		{"USE1-MP:USE1_InputTokenCount_Global_Batch-Units", "input", true},
		{"USE1-MP:USE1_OutputTokenCount-Units", "output", true},
		{"USE1-MP:USE1_OutputTokenCount_Global-Units", "output", true},
		{"USE1-MP:USE1_search_units-Units", "search", true},
		// SKUs that must be skipped:
		{"USE1-MP:USE1_InputImageCount-Units", "", false},
		{"USE1-MP:USE1_ProvisionedThroughput_1MonthCommit_ModelUnits_Usage-Units", "", false},
		{"USE1-MP:USE1_created_image-Units", "", false},
		// Cache SKUs are handled by marketplaceCacheOp before this classifier; see TestMarketplaceCacheOp.
	}
	for _, tt := range tests {
		t.Run(tt.usagetype, func(t *testing.T) {
			direction, ok := classifyMarketplaceUsageType(tt.usagetype)
			assert.Equal(t, tt.wantDirection, direction)
			assert.Equal(t, tt.wantOK, ok)
		})
	}
}

func TestMarketplaceTier(t *testing.T) {
	// region_tier and quota_tier are orthogonal and captured independently; the cache token_type
	// is handled separately by marketplaceCacheOp, so it does not appear here.
	tests := []struct {
		usagetype      string
		wantRegionTier string
		wantQuotaTier  string
	}{
		{"USE1-MP:USE1_InputTokenCount-Units", "in", "standard"},
		{"USE1-MP:USE1_InputTokenCount_Global-Units", "cross", "standard"},
		{"USE1-MP:USE1_InputTokenCount_Batch-Units", "in", "batch"},
		{"USE1-MP:USE1_InputTokenCount_Global_Batch-Units", "cross", "batch"},
		{"USE1-MP:USE1_OutputTokenCount_Global-Units", "cross", "standard"},
		{"USE1-MP:USE1_search_units-Units", "in", "standard"},
		{"USE1-MP:USE1_InputTokenCount_LatencyOptimized-Units", "in", "latency_optimized"},
		// Cross-region is captured independently of the cache operation.
		{"USE1-MP:USE1_CacheReadInputTokenCount_Global-Units", "cross", "standard"},
		{"USE1-MP:USE1_CacheWrite1hInputTokenCount_Global-Units", "cross", "standard"},
	}
	for _, tt := range tests {
		t.Run(tt.usagetype, func(t *testing.T) {
			regionTier, quotaTier := marketplaceTier(tt.usagetype)
			assert.Equal(t, tt.wantRegionTier, regionTier)
			assert.Equal(t, tt.wantQuotaTier, quotaTier)
		})
	}
}

func TestMarketplaceCacheOp(t *testing.T) {
	tests := []struct {
		usagetype     string
		wantTokenType string
		wantCacheTTL  string
		wantSkip      bool
	}{
		{"USE1-MP:USE1_CacheReadInputTokenCount-Units", "cache_read", "", false},
		{"USE1-MP:USE1_CacheReadInputTokenCount_Global-Units", "cache_read", "", false},
		{"USE1-MP:USE1_CacheWriteInputTokenCount-Units", "cache_write", "5m", false},
		{"USE1-MP:USE1_CacheWrite1hInputTokenCount-Units", "cache_write", "1h", false},
		{"USE1-MP:USE1_cache_write_tokens_1h_standard-Units", "cache_write", "1h", false},
		{"USE1-MP:USE1_InputTokenCount-Units", "", "", false}, // not cache, classified normally
		{"USE1-MP:USE1_CacheStorage-Units", "", "", true},     // storage: skipped
		// A cache shape that is neither read nor write must be skipped, not labeled a 5m write.
		{"USE1-MP:USE1_CacheValidationCount-Units", "", "", true},
		// A write with an unrecognized TTL must be skipped, not defaulted to 5m.
		{"USE1-MP:USE1_CacheWrite30mInputTokenCount-Units", "", "", true},
		// An explicit 5m write is recognized as 5m.
		{"USE1-MP:USE1_CacheWrite5mInputTokenCount-Units", "cache_write", "5m", false},
	}
	for _, tt := range tests {
		t.Run(tt.usagetype, func(t *testing.T) {
			tokenType, cacheTTL, skip := marketplaceCacheOp(tt.usagetype)
			assert.Equal(t, tt.wantTokenType, tokenType)
			assert.Equal(t, tt.wantCacheTTL, cacheTTL)
			assert.Equal(t, tt.wantSkip, skip)
		})
	}
}

func TestCollect_EmitsMarketplaceCacheMetrics(t *testing.T) {
	// Cache read/write are emitted on the token metric with token_type=cache_read/cache_write and
	// a cache_ttl label; storage (per token-hour) is skipped.
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	pricingClient := mockclient.NewMockClient(ctrl)
	pricingClient.EXPECT().
		ListBedrockPrices(gomock.Any(), "us-east-1").
		Return([]string{}, nil).
		Times(1)
	pricingClient.EXPECT().
		ListBedrockMarketplacePrices(gomock.Any(), "us-east-1").
		Return([]string{
			marketplacePriceJSON("Claude Sonnet 4.6 (Amazon Bedrock Edition)", "USE1-MP:USE1_CacheReadInputTokenCount-Units", "us-east-1", "0.3"),
			marketplacePriceJSON("Claude Sonnet 4.6 (Amazon Bedrock Edition)", "USE1-MP:USE1_CacheWriteInputTokenCount-Units", "us-east-1", "3.75"),
			marketplacePriceJSON("Claude Sonnet 4.6 (Amazon Bedrock Edition)", "USE1-MP:USE1_CacheWrite1hInputTokenCount_Global-Units", "us-east-1", "6.0"),
			// Storage SKU must be skipped.
			marketplacePriceJSON("Claude Sonnet 4.6 (Amazon Bedrock Edition)", "USE1-MP:USE1_CacheStorage-Units", "us-east-1", "0.07"),
		}, nil).
		Times(1)

	collector, err := New(t.Context(), &Config{
		Regions:       []ec2types.Region{{RegionName: aws.String("us-east-1")}},
		PricingClient: pricingClient,
		AccountID:     "123456789012",
	}, testLogger())
	require.NoError(t, err)

	results, err := collectMetricResults(t, collector)
	require.NoError(t, err)
	require.Len(t, results, 3, "read + write_5m + write_1h emitted; storage skipped")

	// Key each cache price by (region_tier, token_type, cache_ttl) so the orthogonal labels are
	// asserted directly instead of a composed string.
	byKey := map[string]float64{}
	for _, r := range results {
		require.Equal(t, "cloudcost_aws_bedrock_usd_per_1k_tokens", r.FqName)
		require.Equal(t, "claude-sonnet-4.6", r.Labels["model_id"])
		key := r.Labels["region_tier"] + "/" + r.Labels["token_type"] + "/" + r.Labels["cache_ttl"]
		byKey[key] = r.Value
	}
	assert.InDelta(t, 0.0003, byKey["in/cache_read/"], 1e-9)      // 0.3/1M, reads carry no TTL
	assert.InDelta(t, 0.00375, byKey["in/cache_write/5m"], 1e-9)  // 3.75/1M
	assert.InDelta(t, 0.006, byKey["cross/cache_write/1h"], 1e-9) // 6.0/1M, cross-region 1h write
}

func TestCollect_EmitsMarketplaceTokenMetrics(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	pricingClient := mockclient.NewMockClient(ctrl)
	pricingClient.EXPECT().
		ListBedrockPrices(gomock.Any(), "us-east-1").
		Return([]string{}, nil).
		Times(1)
	pricingClient.EXPECT().
		ListBedrockMarketplacePrices(gomock.Any(), "us-east-1").
		Return([]string{
			// Claude Sonnet 4.6: $3.00/M input, $15.00/M output — expect $0.003/1K and $0.015/1K
			marketplacePriceJSON("Claude Sonnet 4.6 (Amazon Bedrock Edition)", "USE1-MP:USE1_InputTokenCount-Units", "us-east-1", "3.0"),
			marketplacePriceJSON("Claude Sonnet 4.6 (Amazon Bedrock Edition)", "USE1-MP:USE1_OutputTokenCount-Units", "us-east-1", "15.0"),
		}, nil).
		Times(1)

	collector, err := New(t.Context(), &Config{
		Regions:       []ec2types.Region{{RegionName: aws.String("us-east-1")}},
		PricingClient: pricingClient,
		AccountID:     "123456789012",
	}, testLogger())
	require.NoError(t, err)

	results, err := collectMetricResults(t, collector)
	require.NoError(t, err)
	require.Len(t, results, 2)

	inputMetric := tokenMetricByType(results, "input")
	require.NotNil(t, inputMetric)
	assert.Equal(t, "claude-sonnet-4.6", inputMetric.Labels["model_id"])
	assert.Equal(t, "anthropic", inputMetric.Labels["family"])
	assert.Equal(t, "in", inputMetric.Labels["region_tier"])
	assert.Equal(t, "standard", inputMetric.Labels["quota_tier"])
	assert.InDelta(t, 0.003, inputMetric.Value, 1e-9)

	outputMetric := tokenMetricByType(results, "output")
	require.NotNil(t, outputMetric)
	assert.InDelta(t, 0.015, outputMetric.Value, 1e-9)
}

func TestCollect_EmitsMarketplaceSearchUnitMetrics(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	pricingClient := mockclient.NewMockClient(ctrl)
	pricingClient.EXPECT().
		ListBedrockPrices(gomock.Any(), "us-east-1").
		Return([]string{}, nil).
		Times(1)
	pricingClient.EXPECT().
		ListBedrockMarketplacePrices(gomock.Any(), "us-east-1").
		Return([]string{
			// Cohere Rerank v3.5: $0.002/search unit — expect $2.00/1K search units
			marketplacePriceJSON("Cohere Rerank v3.5 (Amazon Bedrock Edition)", "USE1-MP:USE1_search_units-Units", "us-east-1", "0.002"),
		}, nil).
		Times(1)

	collector, err := New(t.Context(), &Config{
		Regions:       []ec2types.Region{{RegionName: aws.String("us-east-1")}},
		PricingClient: pricingClient,
		AccountID:     "123456789012",
	}, testLogger())
	require.NoError(t, err)

	results, err := collectMetricResults(t, collector)
	require.NoError(t, err)
	require.Len(t, results, 1)

	m := results[0]
	assert.Equal(t, "cloudcost_aws_bedrock_search_unit_usd_per_1k_search_units", m.FqName)
	assert.Equal(t, "cohere-rerank-v3.5", m.Labels["model_id"])
	assert.Equal(t, "cohere", m.Labels["family"])
	assert.Equal(t, "in", m.Labels["region_tier"])
	assert.Equal(t, "standard", m.Labels["quota_tier"])
	assert.InDelta(t, 2.0, m.Value, 1e-9)
}

func TestCollect_MarketplaceFamilyFilterApplied(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	pricingClient := mockclient.NewMockClient(ctrl)
	pricingClient.EXPECT().
		ListBedrockPrices(gomock.Any(), "us-east-1").
		Return([]string{}, nil).
		Times(1)
	pricingClient.EXPECT().
		ListBedrockMarketplacePrices(gomock.Any(), "us-east-1").
		Return([]string{
			marketplacePriceJSON("Claude Sonnet 4.6 (Amazon Bedrock Edition)", "USE1-MP:USE1_InputTokenCount-Units", "us-east-1", "3.0"),
			marketplacePriceJSON("Cohere Rerank v3.5 (Amazon Bedrock Edition)", "USE1-MP:USE1_search_units-Units", "us-east-1", "0.002"),
		}, nil).
		Times(1)

	collector, err := New(t.Context(), &Config{
		Regions:       []ec2types.Region{{RegionName: aws.String("us-east-1")}},
		PricingClient: pricingClient,
		FamilyFilter:  "anthropic",
		AccountID:     "123456789012",
	}, testLogger())
	require.NoError(t, err)

	results, err := collectMetricResults(t, collector)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "anthropic", results[0].Labels["family"])
}

func TestNew_DegradesToStandardPricingWhenMarketplaceAPIUnavailable(t *testing.T) {
	// Marketplace pricing is best-effort: a marketplace API failure must not drop standard
	// Bedrock pricing or fail collector creation.
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	pricingClient := mockclient.NewMockClient(ctrl)
	pricingClient.EXPECT().
		ListBedrockPrices(gomock.Any(), "us-east-1").
		Return([]string{inputPriceJSON("us-east-1", "USE1", "Claude3Sonnet", "Anthropic", "0.00300")}, nil).
		Times(1)
	pricingClient.EXPECT().
		ListBedrockMarketplacePrices(gomock.Any(), "us-east-1").
		Return(nil, fmt.Errorf("marketplace API unavailable")).
		Times(1)

	collector, err := New(t.Context(), &Config{
		Regions:       []ec2types.Region{{RegionName: aws.String("us-east-1")}},
		PricingClient: pricingClient,
		AccountID:     "123456789012",
	}, testLogger())
	require.NoError(t, err)
	require.NotNil(t, collector)

	results, err := collectMetricResults(t, collector)
	require.NoError(t, err)

	// Standard pricing still flows through despite the marketplace failure.
	inputMetric := tokenMetricByType(results, "input")
	require.NotNil(t, inputMetric)
	assert.Equal(t, "claude3sonnet", inputMetric.Labels["model_id"])
}
