package networking

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/grafana/cloudcost-exporter/pkg/google/client"
	"github.com/grafana/cloudcost-exporter/pkg/utils"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	computev1 "google.golang.org/api/compute/v1"
	"google.golang.org/api/option"
)

var logger = slog.New(slog.NewTextHandler(os.Stdout, nil))

func newemptyPricingMap() *pricingMap {
	return &pricingMap{
		pricing: make(map[string]*pricing),
		logger:  logger,
	}
}

func newTestCollector(pm *pricingMap, t *testing.T) *Collector {
	return &Collector{
		gcpClient:  client.NewMock("project-1", 0, nil, nil, nil, nil, nil),
		projects:   []string{"test-project"},
		pricingMap: pm,
		logger:     logger,
		ctx:        t.Context(),
	}
}

func TestNew(t *testing.T) {
	t.Skip()
}

// newGCPClient creates a mock compute client by serving minimal JSON responses
// for the Google Compute API over an httptest server and wiring a compute.Service to it.
func newGCPClient(t *testing.T, handlers map[string]any) *client.Mock {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if resp, ok := handlers[r.URL.Path]; ok {
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		_ = json.NewEncoder(w).Encode(struct{}{})
	}))
	t.Cleanup(srv.Close)

	computeService, err := computev1.NewService(t.Context(), option.WithoutAuthentication(), option.WithEndpoint(srv.URL))
	require.NoError(t, err)

	return client.NewMock("testing", 0, nil, nil, nil, computeService, nil)
}

func TestCollector_DescribeAndName(t *testing.T) {
	c := &Collector{
		gcpClient:  nil,
		projects:   []string{"testing"},
		pricingMap: newemptyPricingMap(),
		logger:     logger,
		ctx:        t.Context(),
	}

	descCh := make(chan *prometheus.Desc, 3)
	require.NoError(t, c.Describe(descCh))
	close(descCh)

	var descs []*prometheus.Desc
	for d := range descCh {
		descs = append(descs, d)
	}
	require.Len(t, descs, 3)
	assert.Equal(t, collectorName, c.Name())
}

func TestCollector_getForwardingRuleInfo(t *testing.T) {
	handlers := map[string]any{
		"/projects/testing/regions": &computev1.RegionList{Items: []*computev1.Region{{Name: "us-central1"}, {Name: "us-east1"}}},
		"/projects/testing/regions/us-central1/forwardingRules": &computev1.ForwardingRuleList{Items: []*computev1.ForwardingRule{
			{Name: "fr-central-a", IPAddress: "10.0.0.1", LoadBalancingScheme: "EXTERNAL"},
			{Name: "fr-central-b", IPAddress: "10.0.0.2", LoadBalancingScheme: "INTERNAL"},
		}},
		"/projects/testing/regions/us-east1/forwardingRules": &computev1.ForwardingRuleList{Items: []*computev1.ForwardingRule{
			{Name: "fr-east-a", IPAddress: "10.1.0.1", LoadBalancingScheme: "EXTERNAL"},
		}},
	}
	gcpClient := newGCPClient(t, handlers)

	pm := newemptyPricingMap()
	pm.pricing["us-central1"] = &pricing{forwardingRuleCost: 0.03, inboundDataProcessedCost: 0.12, outboundDataProcessedCost: 0.11}
	pm.pricing["us-east1"] = &pricing{forwardingRuleCost: 0.01, inboundDataProcessedCost: 0.05, outboundDataProcessedCost: 0.06}

	c := &Collector{
		gcpClient:  gcpClient,
		projects:   []string{"testing"},
		pricingMap: pm,
		logger:     logger,
		ctx:        t.Context(),
	}

	infos, err := c.getForwardingRuleInfo(t.Context())
	require.NoError(t, err)
	require.Len(t, infos, 3)
	assert.Contains(t, []string{infos[0].Region, infos[1].Region, infos[2].Region}, "us-central1")
	assert.NotEmpty(t, infos[0].IPAddress)
	assert.NotEmpty(t, infos[0].LoadBalancingScheme)
}

func TestCollector_Collect_EmitsMetrics(t *testing.T) {
	handlers := map[string]any{
		"/projects/testing/regions": &computev1.RegionList{Items: []*computev1.Region{{Name: "us-central1"}}},
		"/projects/testing/regions/us-central1/forwardingRules": &computev1.ForwardingRuleList{Items: []*computev1.ForwardingRule{
			{Name: "fr-central-a", IPAddress: "10.0.0.1", LoadBalancingScheme: "EXTERNAL"},
			{Name: "fr-central-b", IPAddress: "10.0.0.2", LoadBalancingScheme: "INTERNAL"},
		}},
	}
	gcpClient := newGCPClient(t, handlers)

	pm := newemptyPricingMap()
	pm.pricing["us-central1"] = &pricing{forwardingRuleCost: 0.03, inboundDataProcessedCost: 0.12, outboundDataProcessedCost: 0.11}

	c := &Collector{
		gcpClient:  gcpClient,
		projects:   []string{"testing"},
		pricingMap: pm,
		logger:     logger,
		ctx:        t.Context(),
	}

	ch := make(chan prometheus.Metric, 10)
	require.NoError(t, c.Collect(t.Context(), ch))
	close(ch)

	var got []*utils.MetricResult
	for m := range ch {
		got = append(got, utils.ReadMetrics(m))
	}
	require.Len(t, got, 6) // 3 metrics per rule x 2 rules

	expected := []*utils.MetricResult{
		{
			FqName: "cloudcost_gcp_clb_forwarding_rule_unit_per_hour",
			Labels: map[string]string{
				"name":                  "fr-central-a",
				"region":                "us-central1",
				"project":               "testing",
				"ip_address":            "10.0.0.1",
				"load_balancing_scheme": "EXTERNAL",
			},
			Value:      0.03,
			MetricType: prometheus.GaugeValue,
		},
		{
			FqName: "cloudcost_gcp_clb_forwarding_rule_inbound_data_processed_per_gib",
			Labels: map[string]string{
				"name":                  "fr-central-a",
				"region":                "us-central1",
				"project":               "testing",
				"ip_address":            "10.0.0.1",
				"load_balancing_scheme": "EXTERNAL",
			},
			Value:      0.12,
			MetricType: prometheus.GaugeValue,
		},
		{
			FqName: "cloudcost_gcp_clb_forwarding_rule_outbound_data_processed_per_gib",
			Labels: map[string]string{
				"name":                  "fr-central-a",
				"region":                "us-central1",
				"project":               "testing",
				"ip_address":            "10.0.0.1",
				"load_balancing_scheme": "EXTERNAL",
			},
			Value:      0.11,
			MetricType: prometheus.GaugeValue,
		},
		{
			FqName: "cloudcost_gcp_clb_forwarding_rule_unit_per_hour",
			Labels: map[string]string{
				"name":                  "fr-central-b",
				"region":                "us-central1",
				"project":               "testing",
				"ip_address":            "10.0.0.2",
				"load_balancing_scheme": "INTERNAL",
			},
			Value:      0.03,
			MetricType: prometheus.GaugeValue,
		},
		{
			FqName: "cloudcost_gcp_clb_forwarding_rule_inbound_data_processed_per_gib",
			Labels: map[string]string{
				"name":                  "fr-central-b",
				"region":                "us-central1",
				"project":               "testing",
				"ip_address":            "10.0.0.2",
				"load_balancing_scheme": "INTERNAL",
			},
			Value:      0.12,
			MetricType: prometheus.GaugeValue,
		},
		{
			FqName: "cloudcost_gcp_clb_forwarding_rule_outbound_data_processed_per_gib",
			Labels: map[string]string{
				"name":                  "fr-central-b",
				"region":                "us-central1",
				"project":               "testing",
				"ip_address":            "10.0.0.2",
				"load_balancing_scheme": "INTERNAL",
			},
			Value:      0.11,
			MetricType: prometheus.GaugeValue,
		},
	}

	assert.ElementsMatch(t, got, expected)
}

func TestProcessForwardingRule(t *testing.T) {
	testTable := map[string]struct {
		pm                                *pricingMap
		expectedForwardingRuleCost        float64
		expectedInboundDataProcessedCost  float64
		expectedOutboundDataProcessedCost float64
	}{
		"empty pricing map, should return 0 for all costs": {
			pm:                                newemptyPricingMap(),
			expectedForwardingRuleCost:        0,
			expectedInboundDataProcessedCost:  0,
			expectedOutboundDataProcessedCost: 0,
		},
	}
	for name, test := range testTable {
		t.Run(name, func(t *testing.T) {
			region := "us-central1"
			fr := &computev1.ForwardingRule{
				Name:                "test-forwarding-rule",
				IPAddress:           "1.2.3.4",
				LoadBalancingScheme: "EXTERNAL",
			}
			c := newTestCollector(test.pm, t)
			info := c.processForwardingRule(fr, region, "test-project")
			require.Equal(t, "test-forwarding-rule", info.Name)
			require.Equal(t, region, info.Region)
			require.Equal(t, "test-project", info.Project)
			require.Equal(t, "1.2.3.4", info.IPAddress)
			require.Equal(t, "EXTERNAL", info.LoadBalancingScheme)
			require.Equal(t, test.expectedForwardingRuleCost, info.ForwardingRuleCost)
			require.Equal(t, test.expectedInboundDataProcessedCost, info.InboundDataProcessedCost)
			require.Equal(t, test.expectedOutboundDataProcessedCost, info.OutboundDataProcessedCost)
		})
	}
}
