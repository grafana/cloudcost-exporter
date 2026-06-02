package google

import (
	"slices"
	"testing"

	"github.com/grafana/cloudcost-exporter/pkg/google/cloudsql"
	"github.com/grafana/cloudcost-exporter/pkg/google/gke"
	gcpmanagedkafka "github.com/grafana/cloudcost-exporter/pkg/google/managedkafka"
	"github.com/grafana/cloudcost-exporter/pkg/google/metrics"
	"github.com/grafana/cloudcost-exporter/pkg/google/networking"
	"github.com/grafana/cloudcost-exporter/pkg/google/vpc"
	"github.com/grafana/cloudcost-exporter/pkg/utils"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

// TestAllGCPCostMetricsHaveProjectLabel guards against regressions where a new
// or modified cost metric ships without the `project` label.
func TestAllGCPCostMetricsHaveProjectLabel(t *testing.T) {
	gcsMetrics := metrics.NewMetrics()

	cases := []struct {
		name string
		desc *prometheus.Desc
	}{
		{"cloudsql.HourlyGaugeDesc", cloudsql.HourlyGaugeDesc},

		{"gke.GKENodeCPUHourlyCostDesc", gke.GKENodeCPUHourlyCostDesc},
		{"gke.GKENodeMemoryHourlyCostDesc", gke.GKENodeMemoryHourlyCostDesc},
		{"gke.PersistentVolumeHourlyCostDesc", gke.PersistentVolumeHourlyCostDesc},

		{"managedkafka.ComputeHourlyGaugeDesc", gcpmanagedkafka.ComputeHourlyGaugeDesc},
		{"managedkafka.StorageHourlyGaugeDesc", gcpmanagedkafka.StorageHourlyGaugeDesc},

		{"networking.ForwardingRuleUnitCostDesc", networking.ForwardingRuleUnitCostDesc},
		{"networking.ForwardingRuleInboundDataProcessedCostDesc", networking.ForwardingRuleInboundDataProcessedCostDesc},
		{"networking.ForwardingRuleOutboundDataProcessedCostDesc", networking.ForwardingRuleOutboundDataProcessedCostDesc},

		{"vpc.CloudNATGatewayHourlyGaugeDesc", vpc.CloudNATGatewayHourlyGaugeDesc},
		{"vpc.CloudNATDataProcessingGaugeDesc", vpc.CloudNATDataProcessingGaugeDesc},
		{"vpc.VPNGatewayHourlyGaugeDesc", vpc.VPNGatewayHourlyGaugeDesc},
		{"vpc.PrivateServiceConnectEndpointHourlyGaugeDesc", vpc.PrivateServiceConnectEndpointHourlyGaugeDesc},
		{"vpc.PrivateServiceConnectDataProcessingGaugeDesc", vpc.PrivateServiceConnectDataProcessingGaugeDesc},

		{"gcs.StorageGauge", descOf(t, gcsMetrics.StorageGauge)},
		{"gcs.StorageDiscountGauge", descOf(t, gcsMetrics.StorageDiscountGauge)},
		{"gcs.OperationsGauge", descOf(t, gcsMetrics.OperationsGauge)},
		{"gcs.OperationsDiscountGauge", descOf(t, gcsMetrics.OperationsDiscountGauge)},
		{"gcs.BucketInfo", descOf(t, gcsMetrics.BucketInfo)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			labels := utils.VariableLabelsFromDesc(tc.desc)
			require.Truef(t, slices.Contains(labels, "project"),
				"cost metric %s must carry a `project` label; got %v", tc.name, labels)
		})
	}
}

// descOf pulls the single Desc out of a GaugeVec by routing its Describe
// through a buffered channel.
func descOf(t *testing.T, c prometheus.Collector) *prometheus.Desc {
	t.Helper()
	ch := make(chan *prometheus.Desc, 1)
	c.Describe(ch)
	close(ch)
	d, ok := <-ch
	require.True(t, ok, "collector did not emit a Desc")
	return d
}
