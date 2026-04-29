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
		Logger:        testLogger(),
		AccountID:     "123456789012",
	})

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
		Logger:        testLogger(),
	})

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
		Logger:        testLogger(),
		AccountID:     "123456789012",
	})
	require.NoError(t, err)

	results, err := collectMetricResults(t, collector)
	require.NoError(t, err)
	require.Len(t, results, 2)

	inputMetric := metricByName(results, "cloudcost_aws_bedrock_token_input_usd_per_1k_tokens")
	require.NotNil(t, inputMetric)
	assert.Equal(t, "us-east-1", inputMetric.Labels["region"])
	assert.Equal(t, "Claude3Sonnet", inputMetric.Labels["model_id"])
	assert.Equal(t, "anthropic", inputMetric.Labels["family"])
	assert.Equal(t, "on_demand", inputMetric.Labels["price_tier"])
	assert.Equal(t, "123456789012", inputMetric.Labels["account_id"])
	assert.InDelta(t, 0.003, inputMetric.Value, 1e-9)

	outputMetric := metricByName(results, "cloudcost_aws_bedrock_token_output_usd_per_1k_tokens")
	require.NotNil(t, outputMetric)
	assert.Equal(t, "us-east-1", outputMetric.Labels["region"])
	assert.Equal(t, "Claude3Sonnet", outputMetric.Labels["model_id"])
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
		Logger:        testLogger(),
		AccountID:     "123456789012",
	})
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
		Logger:        testLogger(),
		AccountID:     "123456789012",
	})
	require.NoError(t, err)

	results, err := collectMetricResults(t, collector)
	require.NoError(t, err)
	require.Len(t, results, 1)

	m := results[0]
	assert.Equal(t, "cross_region", m.Labels["price_tier"])
	assert.Equal(t, "amazon", m.Labels["family"])
	assert.Equal(t, "NovaPremier", m.Labels["model_id"])
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
		Logger:        testLogger(),
		AccountID:     "123456789012",
	})
	require.NoError(t, err)

	results, err := collectMetricResults(t, collector)
	require.NoError(t, err)
	require.Len(t, results, 1)

	m := results[0]
	assert.Equal(t, "on_demand_batch", m.Labels["price_tier"])
	assert.Equal(t, "anthropic", m.Labels["family"])
	assert.Equal(t, "Claude3Sonnet", m.Labels["model_id"])
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
		Logger:        testLogger(),
		AccountID:     "123456789012",
	})
	require.NoError(t, err)

	results, err := collectMetricResults(t, collector)
	require.NoError(t, err)
	require.Len(t, results, 1)

	m := results[0]
	assert.Equal(t, "anthropic", m.Labels["family"])
	assert.Equal(t, "Claude3Sonnet", m.Labels["model_id"])
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
		Logger:        testLogger(),
		AccountID:     "123456789012",
	})
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
		Logger:        testLogger(),
	})
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
		Logger:        testLogger(),
		AccountID:     "123456789012",
	})
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
		Logger:        testLogger(),
		AccountID:     "123456789012",
	})
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

// rawPriceJSON builds a minimal Pricing API JSON item with explicit inferenceType and provider.
func rawPriceJSON(regionCode, usagetype, inferenceType, provider, price string) string {
	return fmt.Sprintf(
		`{"product":{"attributes":{"usagetype":%q,"regionCode":%q,"inferenceType":%q,"provider":%q}},"terms":{"OnDemand":{"term1":{"priceDimensions":{"dim1":{"pricePerUnit":{"USD":%q}}}}}}}`,
		usagetype, regionCode, inferenceType, provider, price,
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
		// Cache tokens contain "InputTokenCount" but must be skipped — explicit guard against misclassification.
		{"USE1-MP:USE1_CacheReadInputTokenCount-Units", "", false},
		{"USE1-MP:USE1_CacheWriteInputTokenCount-Units", "", false},
		{"USE1-MP:USE1_CacheWrite1hInputTokenCount_Global-Units", "", false},
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
		want      string
	}{
		{"USE1-MP:USE1_InputTokenCount-Units", "on_demand"},
		{"USE1-MP:USE1_InputTokenCount_Global-Units", "cross_region"},
		{"USE1-MP:USE1_InputTokenCount_Batch-Units", "on_demand_batch"},
		{"USE1-MP:USE1_InputTokenCount_Global_Batch-Units", "cross_region_batch"},
		{"USE1-MP:USE1_OutputTokenCount_Global-Units", "cross_region"},
		{"USE1-MP:USE1_search_units-Units", "on_demand"},
	}
	for _, tt := range tests {
		t.Run(tt.usagetype, func(t *testing.T) {
			assert.Equal(t, tt.want, extractMarketplacePriceTier(tt.usagetype))
		})
	}
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
		Logger:        testLogger(),
		AccountID:     "123456789012",
	})
	require.NoError(t, err)

	results, err := collectMetricResults(t, collector)
	require.NoError(t, err)
	require.Len(t, results, 2)

	inputMetric := metricByName(results, "cloudcost_aws_bedrock_token_input_usd_per_1k_tokens")
	require.NotNil(t, inputMetric)
	assert.Equal(t, "Claude_Sonnet_4.6", inputMetric.Labels["model_id"])
	assert.Equal(t, "anthropic", inputMetric.Labels["family"])
	assert.Equal(t, "on_demand", inputMetric.Labels["price_tier"])
	assert.InDelta(t, 0.003, inputMetric.Value, 1e-9)

	outputMetric := metricByName(results, "cloudcost_aws_bedrock_token_output_usd_per_1k_tokens")
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
		Logger:        testLogger(),
		AccountID:     "123456789012",
	})
	require.NoError(t, err)

	results, err := collectMetricResults(t, collector)
	require.NoError(t, err)
	require.Len(t, results, 1)

	m := results[0]
	assert.Equal(t, "cloudcost_aws_bedrock_search_unit_usd_per_1k_search_units", m.FqName)
	assert.Equal(t, "Cohere_Rerank_v3.5", m.Labels["model_id"])
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
		Logger:        testLogger(),
		AccountID:     "123456789012",
	})
	require.NoError(t, err)

	results, err := collectMetricResults(t, collector)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "anthropic", results[0].Labels["family"])
}

func TestNew_ReturnsErrorWhenMarketplacePricingAPIUnavailable(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	pricingClient := mockclient.NewMockClient(ctrl)
	pricingClient.EXPECT().
		ListBedrockPrices(gomock.Any(), "us-east-1").
		Return([]string{}, nil).
		Times(1)
	pricingClient.EXPECT().
		ListBedrockMarketplacePrices(gomock.Any(), "us-east-1").
		Return(nil, fmt.Errorf("marketplace API unavailable")).
		Times(1)

	collector, err := New(t.Context(), &Config{
		Regions:       []ec2types.Region{{RegionName: aws.String("us-east-1")}},
		PricingClient: pricingClient,
		Logger:        testLogger(),
	})

	assert.Nil(t, collector)
	require.Error(t, err)
}
