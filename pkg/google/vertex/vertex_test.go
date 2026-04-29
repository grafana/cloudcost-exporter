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

func TestNew_FailsIfPricingInitFails(t *testing.T) {
	_, err := New(t.Context(), &Config{Projects: "test-project"}, testLogger(),
		&stubVertexClient{serviceNameErr: fmt.Errorf("billing API down")})
	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to initialize pricing map")
}

func TestNew_FailsIfNoPricingDataReturned(t *testing.T) {
	_, err := New(t.Context(), &Config{Projects: "test-project"}, testLogger(),
		&stubVertexClient{serviceName: "services/vertex-ai", skus: nil})
	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to initialize pricing map")
}

func TestCollect_EmitsTokenMetrics(t *testing.T) {
	c, err := New(t.Context(), &Config{Projects: "test-project"}, testLogger(),
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

	inputMetric := metricByName(results, "cloudcost_gcp_vertex_token_input_usd_per_1k_tokens")
	require.NotNil(t, inputMetric)
	assert.Equal(t, "gemini-1.5-flash", inputMetric.Labels["model_id"])
	assert.Equal(t, "google", inputMetric.Labels["family"])
	assert.Equal(t, "us-central1", inputMetric.Labels["region"])
	assert.InDelta(t, 0.00125, inputMetric.Value, 1e-9)

	outputMetric := metricByName(results, "cloudcost_gcp_vertex_token_output_usd_per_1k_tokens")
	require.NotNil(t, outputMetric)
	assert.Equal(t, "gemini-1.5-flash", outputMetric.Labels["model_id"])
	assert.Equal(t, "google", outputMetric.Labels["family"])
	assert.Equal(t, "us-central1", outputMetric.Labels["region"])
	assert.InDelta(t, 0.005, outputMetric.Value, 1e-9)
}

func TestFamilyFromModelID(t *testing.T) {
	cases := []struct {
		model  string
		family string
	}{
		{"gemini-1.5-flash", "google"},
		{"gemini-embedding-001", "google"},
		{"claude-3.5-sonnet", "anthropic"},
		{"semantic-ranker-api", "google"},
		{"llama-3-70b", "unknown"},
		{"mistral-large", "unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.model, func(t *testing.T) {
			assert.Equal(t, tc.family, familyFromModelID(tc.model))
		})
	}
}

func TestCollect_EmitsAnthropicFamilyForClaudeTokens(t *testing.T) {
	c, err := New(t.Context(), &Config{Projects: "test-project"}, testLogger(),
		&stubVertexClient{
			serviceName: "services/vertex-ai",
			skus: []*billingpb.Sku{
				newTokenSKU("Claude 3.5 Sonnet Input tokens", "us-east5", "k{char}", 0, 3000000),
			},
		})
	require.NoError(t, err)

	results, err := collectVertexMetrics(t, c)
	require.NoError(t, err)

	inputMetric := metricByName(results, "cloudcost_gcp_vertex_token_input_usd_per_1k_tokens")
	require.NotNil(t, inputMetric)
	assert.Equal(t, "claude-3.5-sonnet", inputMetric.Labels["model_id"])
	assert.Equal(t, "anthropic", inputMetric.Labels["family"])
}

func TestCollect_EmitsComputeMetrics(t *testing.T) {
	c, err := New(t.Context(), &Config{Projects: "test-project"}, testLogger(),
		&stubVertexClient{
			serviceName: "services/vertex-ai",
			skus: []*billingpb.Sku{
				newComputeSKU("Custom Training n1-standard-4 running in us-central1", "us-central1", 0, 500000000),
				newComputeSKU("Spot Custom Training n1-standard-4 running in us-central1", "us-central1", 0, 150000000),
			},
		})
	require.NoError(t, err)

	results, err := collectVertexMetrics(t, c)
	require.NoError(t, err)
	require.Len(t, results, 2)

	onDemand := metricByLabel(results, "cloudcost_gcp_vertex_instance_total_usd_per_hour", "price_tier", "on_demand")
	require.NotNil(t, onDemand)
	assert.Equal(t, "n1-standard-4", onDemand.Labels["machine_type"])
	assert.Equal(t, "training", onDemand.Labels["use_case"])
	assert.Equal(t, "us-central1", onDemand.Labels["region"])
	assert.InDelta(t, 0.5, onDemand.Value, 1e-9)

	spot := metricByLabel(results, "cloudcost_gcp_vertex_instance_total_usd_per_hour", "price_tier", "spot")
	require.NotNil(t, spot)
	assert.InDelta(t, 0.15, spot.Value, 1e-9)
}

func TestCollect_EmitsRerankingMetrics(t *testing.T) {
	c, err := New(t.Context(), &Config{Projects: "test-project"}, testLogger(),
		&stubVertexClient{
			serviceName: "services/vertex-ai",
			skus: []*billingpb.Sku{
				newTokenSKU("Gemini 1.5 Flash Input tokens", "us-central1", "k{char}", 0, 1250000),
			},
			deSkus: []*billingpb.Sku{
				newTokenSKU("Semantic Ranker API Ranking Requests", "global", "k{request}", 0, 1000000),
			},
		})
	require.NoError(t, err)

	results, err := collectVertexMetrics(t, c)
	require.NoError(t, err)

	rerank := metricByName(results, "cloudcost_gcp_vertex_search_unit_usd_per_1k_search_units")
	require.NotNil(t, rerank)
	assert.Equal(t, "semantic-ranker-api", rerank.Labels["model_id"])
	assert.Equal(t, "google", rerank.Labels["family"]) // "semantic" prefix maps to google
	assert.Equal(t, "global", rerank.Labels["region"])
	assert.InDelta(t, 0.001, rerank.Value, 1e-9)
}

func TestCollect_ContextCancellation(t *testing.T) {
	c, err := New(t.Context(), &Config{Projects: "test-project"}, testLogger(),
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
	serviceName    string
	serviceNameErr error
	skus           []*billingpb.Sku
	deSkus         []*billingpb.Sku // Discovery Engine SKUs
}

func (s *stubVertexClient) GetServiceName(_ context.Context, svc string) (string, error) {
	if s.serviceNameErr != nil {
		return "", s.serviceNameErr
	}
	if svc == discoveryEngineServiceName {
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

func metricByLabel(metrics []*utils.MetricResult, fqName, labelKey, labelValue string) *utils.MetricResult {
	for _, m := range metrics {
		if m.FqName == fqName && m.Labels[labelKey] == labelValue {
			return m
		}
	}
	return nil
}
