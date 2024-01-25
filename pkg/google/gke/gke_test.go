package gke

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	billingv1 "cloud.google.com/go/billing/apiv1"
	"cloud.google.com/go/billing/apiv1/billingpb"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	computev1 "google.golang.org/api/compute/v1"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/grafana/cloudcost-exporter/pkg/google/billing"
	"github.com/grafana/cloudcost-exporter/pkg/google/compute"
	"github.com/grafana/cloudcost-exporter/pkg/utils"
)

func TestCollector_Collect(t *testing.T) {
	tests := map[string]struct {
		config          *Config
		testServer      *httptest.Server
		err             error
		collectResponse float64
		expectedMetrics []*utils.MetricResult
	}{
		"Handle http error": {
			config: &Config{
				Projects: "testing",
			},
			testServer: httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			})),
			err:             compute.ListInstancesError,
			collectResponse: 0,
			expectedMetrics: []*utils.MetricResult{},
		},
		"Parse our regular response": {
			config: &Config{
				Projects: "testing,testing-1",
			},
			collectResponse: 1.0,
			expectedMetrics: []*utils.MetricResult{
				{
					FqName: "cloudcost_gcp_gke_node_cpu_usd_per_core_hour",
					Labels: map[string]string{
						"family":       "n1",
						"instance":     "test-n1",
						"machine_type": "n1-slim",
						"price_tier":   "ondemand",
						"project":      "testing",
						"region":       "us-central1",
						"cluster_name": "test",
					},
					Value:      1,
					MetricType: prometheus.GaugeValue,
				},
				{
					FqName: "cloudcost_gcp_gke_node_memory_usd_per_gib_hour",
					Labels: map[string]string{
						"family":       "n1",
						"instance":     "test-n1",
						"machine_type": "n1-slim",
						"price_tier":   "ondemand",
						"project":      "testing",
						"region":       "us-central1",
						"cluster_name": "test",
					},
					Value:      1,
					MetricType: prometheus.GaugeValue,
				},
				{
					FqName: "cloudcost_gcp_gke_node_cpu_usd_per_core_hour",
					Labels: map[string]string{
						"family":       "n2",
						"instance":     "test-n2",
						"machine_type": "n2-slim",
						"price_tier":   "ondemand",
						"project":      "testing",
						"region":       "us-central1",
						"cluster_name": "test",
					},
					Value:      1,
					MetricType: prometheus.GaugeValue,
				},
				{
					FqName: "cloudcost_gcp_gke_node_memory_usd_per_gib_hour",
					Labels: map[string]string{
						"family":       "n2",
						"instance":     "test-n2",
						"machine_type": "n2-slim",
						"price_tier":   "ondemand",
						"project":      "testing",
						"region":       "us-central1",
						"cluster_name": "test",
					},
					Value:      1,
					MetricType: prometheus.GaugeValue,
				},
				{
					FqName: "cloudcost_gcp_gke_node_cpu_usd_per_core_hour",
					Labels: map[string]string{
						"family":       "n1",
						"instance":     "test-n1-spot",
						"machine_type": "n1-slim",
						"price_tier":   "spot",
						"project":      "testing",
						"region":       "us-central1",
						"cluster_name": "test",
					},
					Value:      1,
					MetricType: prometheus.GaugeValue,
				},
				{
					FqName: "cloudcost_gcp_gke_node_memory_usd_per_gib_hour",
					Labels: map[string]string{
						"family":       "n1",
						"instance":     "test-n1-spot",
						"machine_type": "n1-slim",
						"price_tier":   "spot",
						"project":      "testing",
						"region":       "us-central1",
						"cluster_name": "test",
					},
					Value:      1,
					MetricType: prometheus.GaugeValue,
				},

				{
					FqName: "cloudcost_gcp_gke_node_cpu_usd_per_core_hour",
					Labels: map[string]string{
						"family":       "n1",
						"instance":     "test-n1",
						"machine_type": "n1-slim",
						"price_tier":   "ondemand",
						"project":      "testing-1",
						"region":       "us-central1",
						"cluster_name": "test",
					},
					Value:      1,
					MetricType: prometheus.GaugeValue,
				},
				{
					FqName: "cloudcost_gcp_gke_node_memory_usd_per_gib_hour",
					Labels: map[string]string{
						"family":       "n1",
						"instance":     "test-n1",
						"machine_type": "n1-slim",
						"price_tier":   "ondemand",
						"project":      "testing-1",
						"region":       "us-central1",
						"cluster_name": "test",
					},
					Value:      1,
					MetricType: prometheus.GaugeValue,
				},
				{
					FqName: "cloudcost_gcp_gke_node_cpu_usd_per_core_hour",
					Labels: map[string]string{
						"family":       "n2",
						"instance":     "test-n2",
						"machine_type": "n2-slim",
						"price_tier":   "ondemand",
						"project":      "testing-1",
						"region":       "us-central1",
						"cluster_name": "test",
					},
					Value:      1,
					MetricType: prometheus.GaugeValue,
				},
				{
					FqName: "cloudcost_gcp_gke_node_memory_usd_per_gib_hour",
					Labels: map[string]string{
						"family":       "n2",
						"instance":     "test-n2",
						"machine_type": "n2-slim",
						"price_tier":   "ondemand",
						"project":      "testing-1",
						"region":       "us-central1",
						"cluster_name": "test",
					},
					Value:      1,
					MetricType: prometheus.GaugeValue,
				},
				{
					FqName: "cloudcost_gcp_gke_node_cpu_usd_per_core_hour",
					Labels: map[string]string{
						"family":       "n1",
						"instance":     "test-n1-spot",
						"machine_type": "n1-slim",
						"price_tier":   "spot",
						"project":      "testing-1",
						"region":       "us-central1",
						"cluster_name": "test",
					},
					Value:      1,
					MetricType: prometheus.GaugeValue,
				},
				{
					FqName: "cloudcost_gcp_gke_node_memory_usd_per_gib_hour",
					Labels: map[string]string{
						"family":       "n1",
						"instance":     "test-n1-spot",
						"machine_type": "n1-slim",
						"price_tier":   "spot",
						"project":      "testing-1",
						"region":       "us-central1",
						"cluster_name": "test",
					},
					Value:      1,
					MetricType: prometheus.GaugeValue,
				},
			},
			testServer: httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				buf := &computev1.InstanceAggregatedList{
					Items: map[string]computev1.InstancesScopedList{
						"projects/testing/zones/us-central1-a": {
							Instances: []*computev1.Instance{
								{
									Name:        "test-n1",
									MachineType: "abc/n1-slim",
									Zone:        "testing/us-central1-a",
									Scheduling: &computev1.Scheduling{
										ProvisioningModel: "test",
									},
									Labels: map[string]string{
										compute.GkeClusterLabel: "test",
									},
								},
								{
									Name:        "test-n2",
									MachineType: "abc/n2-slim",
									Zone:        "testing/us-central1-a",
									Scheduling: &computev1.Scheduling{
										ProvisioningModel: "test",
									},
									Labels: map[string]string{
										compute.GkeClusterLabel: "test",
									},
								},
								{
									Name:        "test-n1-spot",
									MachineType: "abc/n1-slim",
									Zone:        "testing/us-central1-a",
									Scheduling: &computev1.Scheduling{
										ProvisioningModel: "SPOT",
									},
									Labels: map[string]string{
										compute.GkeClusterLabel: "test",
									},
								},
							},
						},
					},
				}
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(buf)
			})),
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			computeService, err := computev1.NewService(context.Background(), option.WithoutAuthentication(), option.WithEndpoint(test.testServer.URL))
			require.NoError(t, err)
			l, err := net.Listen("tcp", "localhost:0")
			require.NoError(t, err)
			gsrv := grpc.NewServer()
			defer gsrv.Stop()
			go func() {
				if err := gsrv.Serve(l); err != nil {
					t.Errorf("Failed to serve: %v", err)
				}
			}()
			billingpb.RegisterCloudCatalogServer(gsrv, &billing.FakeCloudCatalogServer{})
			cloudCatalogClient, err := billingv1.NewCloudCatalogClient(context.Background(),
				option.WithEndpoint(l.Addr().String()),
				option.WithoutAuthentication(),
				option.WithGRPCDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())),
			)
			require.NoError(t, err)
			collector := New(test.config, computeService, cloudCatalogClient)
			require.NotNil(t, collector)
			ch := make(chan prometheus.Metric)
			go func() {
				if up := collector.CollectMetrics(ch); up != test.collectResponse {
					t.Errorf("expected 1, got %v", up)
				}
				close(ch)
			}()

			for _, expectedMetric := range test.expectedMetrics {
				m := utils.ReadMetrics(<-ch)
				require.Equal(t, expectedMetric, m)
			}
		})
	}
}
