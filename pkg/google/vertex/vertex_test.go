package vertex

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"

	"cloud.google.com/go/billing/apiv1/billingpb"
	managedkafkapb "cloud.google.com/go/managedkafka/apiv1/managedkafkapb"
	"github.com/grafana/cloudcost-exporter/pkg/google/client"
	gcmetrics "github.com/grafana/cloudcost-exporter/pkg/google/metrics"
	"github.com/grafana/cloudcost-exporter/pkg/utils"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/compute/v1"
	sqladmin "google.golang.org/api/sqladmin/v1beta4"
)

func TestNew_FailsIfProjectIDEmpty(t *testing.T) {
	_, err := New(t.Context(), &Config{ProjectId: ""}, testLogger(), &stubVertexClient{})
	require.Error(t, err)
	assert.ErrorContains(t, err, "projectID cannot be empty")
}

func TestNew_FailsIfPricingInitFails(t *testing.T) {
	_, err := New(t.Context(), testConfig(), testLogger(),
		&stubVertexClient{serviceNameErr: fmt.Errorf("billing API down")})
	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to initialize pricing map")
}

func TestNew_FailsIfNoPricingDataReturned(t *testing.T) {
	_, err := New(t.Context(), testConfig(), testLogger(),
		&stubVertexClient{serviceName: "services/vertex-ai", skus: nil})
	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to initialize pricing map")
}

func TestCollect_EmitsTokenMetrics(t *testing.T) {
	c, err := New(t.Context(), testConfig(), testLogger(),
		&stubVertexClient{
			serviceName: "services/vertex-ai",
			skus: []*billingpb.Sku{
				newTokenSKU("Gemini 1.5 Flash Input tokens", "us-central1", "k{char}", 0, 1250000),
				newTokenSKU("Gemini 1.5 Flash Output tokens", "us-central1", "k{char}", 0, 5000000),
			},
		})
	require.NoError(t, err)

	results, err := collectVertexMetrics(t, c)
	require.NoError(t, err)
	require.Len(t, results, 2)

	inputMetric := metricByLabel(results, "cloudcost_gcp_vertex_usd_per_1k_tokens", "gen_ai_token_type", "input")
	require.NotNil(t, inputMetric)
	assert.Equal(t, testProject, inputMetric.Labels["project_id"])
	assert.Equal(t, "gemini-1.5-flash", inputMetric.Labels["gen_ai_request_model"])
	assert.Equal(t, "google", inputMetric.Labels["family"])
	assert.Equal(t, "us-central1", inputMetric.Labels["region"])
	assert.Equal(t, "on_demand", inputMetric.Labels["price_tier"])
	assert.InDelta(t, 0.00125, inputMetric.Value, 1e-9)

	outputMetric := metricByLabel(results, "cloudcost_gcp_vertex_usd_per_1k_tokens", "gen_ai_token_type", "output")
	require.NotNil(t, outputMetric)
	assert.Equal(t, testProject, outputMetric.Labels["project_id"])
	assert.Equal(t, "gemini-1.5-flash", outputMetric.Labels["gen_ai_request_model"])
	assert.Equal(t, "google", outputMetric.Labels["family"])
	assert.Equal(t, "us-central1", outputMetric.Labels["region"])
	assert.Equal(t, "on_demand", outputMetric.Labels["price_tier"])
	assert.InDelta(t, 0.005, outputMetric.Value, 1e-9)
}

func TestCollect_StampsSingleAuthProjectID(t *testing.T) {
	c, err := New(t.Context(), &Config{ProjectId: "auth-project"}, testLogger(),
		&stubVertexClient{
			serviceName: "services/vertex-ai",
			skus: []*billingpb.Sku{
				newTokenSKU("Gemini 1.5 Flash Input tokens", "us-central1", "k{char}", 0, 1250000),
			},
		})
	require.NoError(t, err)

	results, err := collectVertexMetrics(t, c)
	require.NoError(t, err)

	// Prices are project-independent: every series carries the single auth project_id (like
	// Bedrock's single account_id), not a per-project fan-out.
	require.Len(t, results, 1)
	assert.Equal(t, "auth-project", results[0].Labels["project_id"])
}

func TestCollect_EmitsClaudeOnVertexPrices(t *testing.T) {
	stub := &stubVertexClient{
		serviceName:    "services/vertex-ai",
		billingAccount: "test-account",
		skus:           []*billingpb.Sku{newTokenSKU("Gemini 1.5 Flash Input tokens", "us-central1", "k{char}", 0, 1250000)},
		claudePrices: []client.BillingAccountPrice{
			{Description: "Claude Sonnet 4.6 — Input Tokens — global — Context Window Size from 0 to 200000 Tokens", USDPerUnit: 3, UnitQuantity: 1_000_000},
			{Description: "Claude Sonnet 4.6 — Input Cache Read Tokens — global — Context Window Size from 0 to 200000 Tokens", USDPerUnit: 0.3, UnitQuantity: 1_000_000},
		},
	}
	// In production the account-scoped Claude prices load off the startup path (New populates the
	// catalog, then a background refresh adds Claude). Run a full Populate synchronously here so the
	// Claude prices are deterministically present.
	pm, err := NewPricingMap(t.Context(), testLogger(), stub, nil, "test-account")
	require.NoError(t, err)
	require.NoError(t, pm.Populate(t.Context()))
	c := &Collector{pricingMap: pm, logger: testLogger(), projectID: testProject}

	results, err := collectVertexMetrics(t, c)
	require.NoError(t, err)

	// The Claude series carries family=anthropic and the Sigil-matching model slug.
	claudeInput := findMetric(results, func(m *utils.MetricResult) bool {
		return m.Labels["family"] == "anthropic" && m.Labels["gen_ai_token_type"] == "input"
	})
	require.NotNil(t, claudeInput)
	assert.Equal(t, "claude-sonnet-4-6", claudeInput.Labels["gen_ai_request_model"])
	assert.InDelta(t, 0.003, claudeInput.Value, 1e-12)

	cacheRead := metricByLabel(results, "cloudcost_gcp_vertex_usd_per_1k_tokens", "gen_ai_token_type", "cache_read")
	require.NotNil(t, cacheRead)
	assert.Equal(t, "claude-sonnet-4-6", cacheRead.Labels["gen_ai_request_model"])
	assert.InDelta(t, 0.0003, cacheRead.Value, 1e-12)
}

func TestCollect_EmitsCharacterMetrics(t *testing.T) {
	c, err := New(t.Context(), testConfig(), testLogger(),
		&stubVertexClient{
			serviceName: "services/vertex-ai",
			skus: []*billingpb.Sku{
				newTokenSKU("Translation LLM Input Characters", "global", "count", 0, 50000),
				newTokenSKU("Translation LLM Output Characters", "global", "count", 0, 150000),
			},
		})
	require.NoError(t, err)

	results, err := collectVertexMetrics(t, c)
	require.NoError(t, err)
	require.Len(t, results, 2)

	inputMetric := metricByLabel(results, "cloudcost_gcp_vertex_usd_per_1k_characters", "gen_ai_token_type", "input")
	require.NotNil(t, inputMetric)
	assert.Equal(t, testProject, inputMetric.Labels["project_id"])
	assert.Equal(t, "translation-llm", inputMetric.Labels["gen_ai_request_model"])
	assert.Equal(t, "google", inputMetric.Labels["family"])
	assert.Equal(t, "global", inputMetric.Labels["region"])
	assert.Equal(t, "on_demand", inputMetric.Labels["price_tier"])
	assert.InDelta(t, 0.05, inputMetric.Value, 1e-9)

	outputMetric := metricByLabel(results, "cloudcost_gcp_vertex_usd_per_1k_characters", "gen_ai_token_type", "output")
	require.NotNil(t, outputMetric)
	assert.Equal(t, testProject, outputMetric.Labels["project_id"])
	assert.Equal(t, "translation-llm", outputMetric.Labels["gen_ai_request_model"])
	assert.Equal(t, "google", outputMetric.Labels["family"])
	assert.Equal(t, "global", outputMetric.Labels["region"])
	assert.Equal(t, "on_demand", outputMetric.Labels["price_tier"])
	assert.InDelta(t, 0.15, outputMetric.Value, 1e-9)
}

func TestFamilyFromModelID(t *testing.T) {
	cases := []struct {
		model  string
		family string
	}{
		{"gemini-1.5-flash", "google"},
		{"gemini-embedding-001", "google"},
		{"gemma-4", "google"},
		{"cloud-vertex-ai-model-garden-model-as-a-service-gemma-4", "google"},
		{"semantic-ranker-api", "google"},
		{"cloud-vertex-ai-model-garden-model-as-a-service-deepseek-r1-0528", "deepseek"},
		{"cloud-vertex-ai-model-garden-model-as-a-service-llama-4-maverick", "meta"},
		{"llama-4-maverick", "meta"},
		{"cloud-vertex-ai-model-garden-model-as-a-service-qwen3-235b-a22b-instruct-2507", "alibaba"},
		{"cloud-vertex-ai-model-garden-model-as-a-service-glm-5", "unknown"},
		{"cloud-vertex-ai-model-garden-model-as-a-service-minimax-m2", "minimax"},
		{"cloud-vertex-ai-model-garden-model-as-a-service-kimi-k2-thinking", "moonshot"},
		{"adaptive-machine-translation", "google"},
		{"translation-llm", "google"},
		{"mistral-large", "unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.model, func(t *testing.T) {
			assert.Equal(t, tc.family, familyFromModelID(tc.model))
		})
	}
}

func TestCollect_EmitsRerankingMetrics(t *testing.T) {
	c, err := New(t.Context(), testConfig(), testLogger(),
		&stubVertexClient{
			serviceName: "services/vertex-ai",
			skus: []*billingpb.Sku{
				newTokenSKU("Gemini 1.5 Flash Input tokens", "us-central1", "k{char}", 0, 1250000),
			},
			deSkus: []*billingpb.Sku{
				// Per-request ("count") price scaled to per-1k: 0.001 -> 1.0.
				newTokenSKU("Vertex AI Search: Ranking", "global", "count", 0, 1000000),
			},
		})
	require.NoError(t, err)

	results, err := collectVertexMetrics(t, c)
	require.NoError(t, err)

	rerank := metricByName(results, "cloudcost_gcp_vertex_search_unit_usd_per_1k_search_units")
	require.NotNil(t, rerank)
	assert.Equal(t, testProject, rerank.Labels["project_id"])
	assert.Equal(t, "semantic-ranker", rerank.Labels["gen_ai_request_model"])
	assert.Equal(t, "google", rerank.Labels["family"]) // "semantic" prefix maps to google
	assert.Equal(t, "global", rerank.Labels["region"])
	assert.Equal(t, "on_demand", rerank.Labels["price_tier"])
	assert.InDelta(t, 1.0, rerank.Value, 1e-9)
}

func TestCollect_DiscoveryEngineUnavailable_RerankingOmitted(t *testing.T) {
	c, err := New(t.Context(), testConfig(), testLogger(),
		&stubVertexClient{
			serviceName:      "services/vertex-ai",
			deServiceNameErr: fmt.Errorf("Discovery Engine Ranking API not found; check that Vertex AI Search is enabled in the GCP project"),
			skus: []*billingpb.Sku{
				newTokenSKU("Gemini 1.5 Flash Input tokens", "us-central1", "k{char}", 0, 1250000),
			},
		})
	require.NoError(t, err)

	results, err := collectVertexMetrics(t, c)
	require.NoError(t, err)

	assert.Nil(t, metricByName(results, "cloudcost_gcp_vertex_search_unit_usd_per_1k_search_units"))
	assert.NotNil(t, metricByName(results, "cloudcost_gcp_vertex_usd_per_1k_tokens"))
}

func TestCollect_ContextCancellation(t *testing.T) {
	c, err := New(t.Context(), testConfig(), testLogger(),
		&stubVertexClient{
			serviceName: "services/vertex-ai",
			skus: []*billingpb.Sku{
				newTokenSKU("Gemini 1.5 Flash Input tokens", "us-central1", "k{char}", 0, 1250000),
			},
		})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	ch := make(chan prometheus.Metric, 10)
	err = c.Collect(ctx, ch)
	assert.ErrorIs(t, err, context.Canceled)
}

// stubVertexClient implements client.Client for tests.
type stubVertexClient struct {
	serviceName      string
	serviceNameErr   error
	deServiceNameErr error
	skus             []*billingpb.Sku
	deSkus           []*billingpb.Sku // Discovery Engine SKUs
	billingAccount   string           // resolved by GetProjectBillingAccount; empty skips the Claude fetch
	claudePrices     []client.BillingAccountPrice
	claudePricesErr  error
}

func (s *stubVertexClient) GetServiceName(_ context.Context, svc string) (string, error) {
	if s.serviceNameErr != nil {
		return "", s.serviceNameErr
	}
	if svc == discoveryEngineServiceName {
		if s.deServiceNameErr != nil {
			return "", s.deServiceNameErr
		}
		return "services/discovery-engine", nil
	}
	return s.serviceName, nil
}

func (s *stubVertexClient) ExportRegionalDiscounts(_ context.Context, _ *gcmetrics.Metrics) error {
	return nil
}

func (s *stubVertexClient) ExportGCPCostData(_ context.Context, _ string, _ *gcmetrics.Metrics) float64 {
	return 0
}

func (s *stubVertexClient) ExportBucketInfo(_ context.Context, _ []string, _ *gcmetrics.Metrics) error {
	return nil
}

func (s *stubVertexClient) GetPricing(_ context.Context, svcName string) []*billingpb.Sku {
	if svcName == "services/discovery-engine" {
		return s.deSkus
	}
	return s.skus
}

func (s *stubVertexClient) ListBillingAccountPrices(_ context.Context, _, _ string) ([]client.BillingAccountPrice, error) {
	return s.claudePrices, s.claudePricesErr
}

func (s *stubVertexClient) GetProjectBillingAccount(_ context.Context, _ string) (string, error) {
	return s.billingAccount, nil
}

func (s *stubVertexClient) GetZones(_ string) ([]*compute.Zone, error) {
	return nil, nil
}

func (s *stubVertexClient) GetRegions(_ string) ([]*compute.Region, error) {
	return nil, nil
}

func (s *stubVertexClient) ListInstancesInZone(_ string, _ string) ([]*client.MachineSpec, error) {
	return nil, nil
}

func (s *stubVertexClient) ListDisks(_ context.Context, _ string, _ string) ([]*compute.Disk, error) {
	return nil, nil
}

func (s *stubVertexClient) ListForwardingRules(_ context.Context, _ string, _ string) ([]*compute.ForwardingRule, error) {
	return nil, nil
}

func (s *stubVertexClient) ListSQLInstances(_ context.Context, _ string) ([]*sqladmin.DatabaseInstance, error) {
	return nil, nil
}

func (s *stubVertexClient) ListManagedKafkaLocations(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

func (s *stubVertexClient) ListManagedKafkaClusters(_ context.Context, _ string, _ string) ([]*managedkafkapb.Cluster, error) {
	return nil, nil
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func testConfig() *Config {
	return &Config{ProjectId: testProject}
}

const testProject = "test-project"

func collectVertexMetrics(t *testing.T, c *Collector) ([]*utils.MetricResult, error) {
	t.Helper()
	ch := make(chan prometheus.Metric, 10)
	if err := c.Collect(t.Context(), ch); err != nil {
		close(ch)
		return nil, err
	}
	close(ch)
	var results []*utils.MetricResult
	for metric := range ch {
		if r := utils.ReadMetrics(metric); r != nil {
			results = append(results, r)
		}
	}
	return results, nil
}

func metricByName(metrics []*utils.MetricResult, fqName string) *utils.MetricResult {
	for _, m := range metrics {
		if m.FqName == fqName {
			return m
		}
	}
	return nil
}

func findMetric(metrics []*utils.MetricResult, pred func(*utils.MetricResult) bool) *utils.MetricResult {
	for _, m := range metrics {
		if pred(m) {
			return m
		}
	}
	return nil
}

func metricByLabel(metrics []*utils.MetricResult, fqName, labelKey, labelValue string) *utils.MetricResult {
	for _, m := range metrics {
		if m.FqName == fqName && m.Labels[labelKey] == labelValue {
			return m
		}
	}
	return nil
}
