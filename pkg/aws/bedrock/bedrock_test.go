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

	inputMetric := metricByName(results, "cloudcost_aws_bedrock_input_usd_per_1k_tokens")
	require.NotNil(t, inputMetric)
	assert.Equal(t, "us-east-1", inputMetric.Labels["region"])
	// These fixtures omit the `model` attribute, so model_id is the normalized usagetype slug
	// (fallback path). Real Claude SKUs carry `model`, yielding claude-3-sonnet; see
	// TestCollect_StandardModelIDUsesModelAttribute.
	assert.Equal(t, "claude3sonnet", inputMetric.Labels["model_id"])
	assert.Equal(t, "anthropic", inputMetric.Labels["family"])
	assert.Equal(t, "on_demand", inputMetric.Labels["price_tier"])
	assert.Equal(t, "123456789012", inputMetric.Labels["account_id"])
	assert.InDelta(t, 0.003, inputMetric.Value, 1e-9)

	outputMetric := metricByName(results, "cloudcost_aws_bedrock_output_usd_per_1k_tokens")
	require.NotNil(t, outputMetric)
	assert.Equal(t, "us-east-1", outputMetric.Labels["region"])
	assert.Equal(t, "claude3sonnet", outputMetric.Labels["model_id"])
	assert.Equal(t, "anthropic", outputMetric.Labels["family"])
	assert.Equal(t, "on_demand", outputMetric.Labels["price_tier"])
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
	assert.Equal(t, "cross_region", m.Labels["price_tier"])
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
	assert.Equal(t, "on_demand_batch", m.Labels["price_tier"])
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
	tests := []struct {
		usagetype     string
		wantModelID   string
		wantPriceTier string
	}{
		{
			"USE1-Claude3Sonnet-input-tokens",
			"Claude3Sonnet", "on_demand",
		},
		{
			"USE1-Claude2.0-input-tokens",
			"Claude2.0", "on_demand",
		},
		{
			"USE1-Llama4-Maverick-17B-input-tokens-batch",
			"Llama4-Maverick-17B", "on_demand_batch",
		},
		{
			"USE1-GPT-OSS-Safeguard-20B-input-tokens-priority",
			"GPT-OSS-Safeguard-20B", "on_demand_priority",
		},
		{
			"USE1-Gemma-3-4B-IT-input-tokens-flex",
			"Gemma-3-4B-IT", "on_demand_flex",
		},
		{
			"USE1-MistralSmall-input-tokens-batch",
			"MistralSmall", "on_demand_batch",
		},
		{
			"USE1-Nova2.0Lite-input-tokens-cross-region-global-batch",
			"Nova2.0Lite", "cross_region",
		},
		{
			"USE1-Nova2.0Pro-text-input-tokens-priority-cross-region-global",
			"Nova2.0Pro", "cross_region",
		},
		{
			"USE1-GPT-OSS-Safeguard-120B-output-tokens-batch",
			"GPT-OSS-Safeguard-120B", "on_demand_batch",
		},
		{
			"USE1-Llama3-2-1B-output-tokens",
			"Llama3-2-1B", "on_demand",
		},
		{
			"USE1-cohere.rerank-english-v3-search-units",
			"cohere.rerank-english-v3", "on_demand",
		},
		// Unrecognized format returns empty model ID.
		{
			"USE1-Guardrail-AutomatedReasoningPolicyUnitsConsumed",
			"", "on_demand",
		},
	}

	for _, tt := range tests {
		t.Run(tt.usagetype, func(t *testing.T) {
			modelID, priceTier := parseBedrockModelID(tt.usagetype)
			assert.Equal(t, tt.wantModelID, modelID)
			assert.Equal(t, tt.wantPriceTier, priceTier)
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

func metricByName(results []*utils.MetricResult, fqName string) *utils.MetricResult {
	for _, result := range results {
		if result.FqName == fqName {
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
		if r.Labels["model_id"] != "claude-3-sonnet" {
			continue
		}
		switch r.FqName {
		case "cloudcost_aws_bedrock_input_usd_per_1k_tokens":
			input = r
		case "cloudcost_aws_bedrock_output_usd_per_1k_tokens":
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

func TestExtractMarketplacePriceTier(t *testing.T) {
	tests := []struct {
		usagetype string
		cacheOp   string
		want      string
	}{
		{"USE1-MP:USE1_InputTokenCount-Units", "", "on_demand"},
		{"USE1-MP:USE1_InputTokenCount_Global-Units", "", "cross_region"},
		{"USE1-MP:USE1_InputTokenCount_Batch-Units", "", "on_demand_batch"},
		{"USE1-MP:USE1_InputTokenCount_Global_Batch-Units", "", "cross_region_batch"},
		{"USE1-MP:USE1_OutputTokenCount_Global-Units", "", "cross_region"},
		{"USE1-MP:USE1_search_units-Units", "", "on_demand"},
		// Latency-optimized is its own quota tier (does not collide with on_demand).
		{"USE1-MP:USE1_InputTokenCount_LatencyOptimized-Units", "", "on_demand_latency_optimized"},
		// Cache operations fold into the tier and stack with cross-region.
		{"USE1-MP:USE1_CacheReadInputTokenCount-Units", "cache_read", "cache_read"},
		{"USE1-MP:USE1_CacheReadInputTokenCount_Global-Units", "cache_read", "cross_region_cache_read"},
		{"USE1-MP:USE1_CacheWriteInputTokenCount-Units", "cache_write_5m", "cache_write_5m"},
		{"USE1-MP:USE1_CacheWrite1hInputTokenCount-Units", "cache_write_1h", "cache_write_1h"},
		{"USE1-MP:USE1_CacheWrite1hInputTokenCount_Global-Units", "cache_write_1h", "cross_region_cache_write_1h"},
	}
	for _, tt := range tests {
		t.Run(tt.usagetype, func(t *testing.T) {
			assert.Equal(t, tt.want, extractMarketplacePriceTier(tt.usagetype, tt.cacheOp))
		})
	}
}

func TestMarketplaceCacheOp(t *testing.T) {
	tests := []struct {
		usagetype string
		wantOp    string
		wantSkip  bool
	}{
		{"USE1-MP:USE1_CacheReadInputTokenCount-Units", "cache_read", false},
		{"USE1-MP:USE1_CacheReadInputTokenCount_Global-Units", "cache_read", false},
		{"USE1-MP:USE1_CacheWriteInputTokenCount-Units", "cache_write_5m", false},
		{"USE1-MP:USE1_CacheWrite1hInputTokenCount-Units", "cache_write_1h", false},
		{"USE1-MP:USE1_cache_write_tokens_1h_standard-Units", "cache_write_1h", false},
		{"USE1-MP:USE1_InputTokenCount-Units", "", false}, // not cache, classified normally
		{"USE1-MP:USE1_CacheStorage-Units", "", true},     // storage: skipped
		// A cache shape that is neither read nor write must be skipped, not labeled a 5m write.
		{"USE1-MP:USE1_CacheValidationCount-Units", "", true},
		// A write with an unrecognized TTL must be skipped, not defaulted to 5m.
		{"USE1-MP:USE1_CacheWrite30mInputTokenCount-Units", "", true},
		// An explicit 5m write is recognized as 5m.
		{"USE1-MP:USE1_CacheWrite5mInputTokenCount-Units", "cache_write_5m", false},
	}
	for _, tt := range tests {
		t.Run(tt.usagetype, func(t *testing.T) {
			op, skip := marketplaceCacheOp(tt.usagetype)
			assert.Equal(t, tt.wantOp, op)
			assert.Equal(t, tt.wantSkip, skip)
		})
	}
}

func TestCollect_EmitsMarketplaceCacheMetrics(t *testing.T) {
	// Cache read/write are emitted on the input metric with cache_* price_tier values; storage
	// (per token-hour) is skipped.
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

	byTier := map[string]float64{}
	for _, r := range results {
		require.Equal(t, "cloudcost_aws_bedrock_input_usd_per_1k_tokens", r.FqName)
		require.Equal(t, "claude-sonnet-4.6", r.Labels["model_id"])
		byTier[r.Labels["price_tier"]] = r.Value
	}
	assert.InDelta(t, 0.0003, byTier["cache_read"], 1e-9)      // 0.3/1M
	assert.InDelta(t, 0.00375, byTier["cache_write_5m"], 1e-9) // 3.75/1M
	assert.InDelta(t, 0.006, byTier["cross_region_cache_write_1h"], 1e-9)
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

	inputMetric := metricByName(results, "cloudcost_aws_bedrock_input_usd_per_1k_tokens")
	require.NotNil(t, inputMetric)
	assert.Equal(t, "claude-sonnet-4.6", inputMetric.Labels["model_id"])
	assert.Equal(t, "anthropic", inputMetric.Labels["family"])
	assert.Equal(t, "on_demand", inputMetric.Labels["price_tier"])
	assert.InDelta(t, 0.003, inputMetric.Value, 1e-9)

	outputMetric := metricByName(results, "cloudcost_aws_bedrock_output_usd_per_1k_tokens")
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
	inputMetric := metricByName(results, "cloudcost_aws_bedrock_input_usd_per_1k_tokens")
	require.NotNil(t, inputMetric)
	assert.Equal(t, "claude3sonnet", inputMetric.Labels["model_id"])
}
