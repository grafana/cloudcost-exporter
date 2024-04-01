package gke

import (
	"context"
	"encoding/json"
	"fmt"
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
					FqName: "cloudcost_gcp_gke_instance_cpu_usd_per_core_hour",
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
					FqName: "cloudcost_gcp_gke_instance_memory_usd_per_gib_hour",
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
					FqName: "cloudcost_gcp_gke_instance_cpu_usd_per_core_hour",
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
					FqName: "cloudcost_gcp_gke_instance_memory_usd_per_gib_hour",
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
					FqName: "cloudcost_gcp_gke_instance_cpu_usd_per_core_hour",
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
					FqName: "cloudcost_gcp_gke_instance_memory_usd_per_gib_hour",
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
					FqName: "cloudcost_gcp_gke_instance_cpu_usd_per_core_hour",
					Labels: map[string]string{
						"family":       "n2",
						"instance":     "test-n2-us-east1",
						"machine_type": "n2-slim",
						"price_tier":   "ondemand",
						"project":      "testing",
						"region":       "us-east1",
						"cluster_name": "test",
					},
					Value:      1,
					MetricType: prometheus.GaugeValue,
				},
				{
					FqName: "cloudcost_gcp_gke_instance_memory_usd_per_gib_hour",
					Labels: map[string]string{
						"family":       "n2",
						"instance":     "test-n2-us-east1",
						"machine_type": "n2-slim",
						"price_tier":   "ondemand",
						"project":      "testing",
						"region":       "us-east1",
						"cluster_name": "test",
					},
					Value:      1,
					MetricType: prometheus.GaugeValue,
				},
				{
					FqName: "cloudcost_gcp_gke_persistent_volume_usd_per_gib_hour",
					Labels: map[string]string{
						"cluster_name":     "test",
						"namespace":        "cloudcost-exporter",
						"persistentvolume": "test-disk",
						"region":           "us-central1",
						"project":          "testing",
						"storage_class":    "pd-standard",
					},
					Value:      0,
					MetricType: prometheus.GaugeValue,
				},
				{
					FqName: "cloudcost_gcp_gke_persistent_volume_usd_per_gib_hour",
					Labels: map[string]string{
						"cluster_name":     "test",
						"namespace":        "cloudcost-exporter",
						"persistentvolume": "test-ssd-disk",
						"region":           "us-east4",
						"project":          "testing",
						"storage_class":    "pd-ssd",
					},
					Value:      0.15359342915811086,
					MetricType: prometheus.GaugeValue,
				},
				{

					FqName: "cloudcost_gcp_gke_instance_cpu_usd_per_core_hour",
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
					FqName: "cloudcost_gcp_gke_instance_memory_usd_per_gib_hour",
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
					FqName: "cloudcost_gcp_gke_instance_cpu_usd_per_core_hour",
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
					FqName: "cloudcost_gcp_gke_instance_memory_usd_per_gib_hour",
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
					FqName: "cloudcost_gcp_gke_instance_cpu_usd_per_core_hour",
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
					FqName: "cloudcost_gcp_gke_instance_memory_usd_per_gib_hour",
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
					FqName: "cloudcost_gcp_gke_instance_cpu_usd_per_core_hour",
					Labels: map[string]string{
						"family":       "n2",
						"instance":     "test-n2-us-east1",
						"machine_type": "n2-slim",
						"price_tier":   "ondemand",
						"project":      "testing-1",
						"region":       "us-east1",
						"cluster_name": "test",
					},
					Value:      1,
					MetricType: prometheus.GaugeValue,
				},
				{
					FqName: "cloudcost_gcp_gke_instance_memory_usd_per_gib_hour",
					Labels: map[string]string{
						"family":       "n2",
						"instance":     "test-n2-us-east1",
						"machine_type": "n2-slim",
						"price_tier":   "ondemand",
						"project":      "testing-1",
						"region":       "us-east1",
						"cluster_name": "test",
					},
					Value:      1,
					MetricType: prometheus.GaugeValue,
				},
			},
			testServer: httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var buf interface{}
				switch r.URL.Path {
				case "/projects/testing/zones/us-central1-a/instances", "/projects/testing-1/zones/us-central1-a/instances":
					buf = &computev1.InstanceList{
						Items: []*computev1.Instance{
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
							{
								Name:        "test-n2-us-east1",
								MachineType: "abc/n2-slim",
								Zone:        "testing/us-east1-a",
								Scheduling: &computev1.Scheduling{
									ProvisioningModel: "test",
								},
								Labels: map[string]string{
									compute.GkeClusterLabel: "test",
								},
							},
						},
					}
				case "/projects/testing/zones", "/projects/testing-1/zones":
					buf = &computev1.ZoneList{
						Items: []*computev1.Zone{
							{
								Name: "us-central1-a",
							}},
					}
				case "/projects/testing/zones/us-central1-a/disks", "/projects/testing-1/zones/us-central1-a/disks":
					buf = &computev1.DiskList{
						Items: []*computev1.Disk{
							{
								Name: "test-disk",
								Zone: "testing/us-central1-a",
								Labels: map[string]string{
									compute.GkeClusterLabel: "test",
								},
								Description: `{"kubernetes.io/created-for/pvc/namespace":"cloudcost-exporter"}`,
								Type:        "pd-standard",
							},
							{
								Name: "test-ssd-disk",
								Zone: "testing/us-east4",
								Labels: map[string]string{
									compute.GkeClusterLabel: "test",
								},
								Description: `{"kubernetes.io/created-for/pvc/namespace":"cloudcost-exporter"}`,
								Type:        "pd-ssd",
								SizeGb:      600,
							},
						},
					}
				default:
					fmt.Println(r.URL.Path)
				}
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

			var metrics []*utils.MetricResult
			for metric := range ch {
				metrics = append(metrics, utils.ReadMetrics(metric))
			}
			if len(metrics) == 0 {
				return
			}

			for i, expectedMetric := range test.expectedMetrics {
				require.Equal(t, expectedMetric, metrics[i])
			}
		})
	}
}

func Test_extractLabelsFromDesc(t *testing.T) {
	tests := map[string]struct {
		description    string
		labels         map[string]string
		expectedLabels map[string]string
		wantErr        bool
	}{
		"Empty description should return an empty map": {
			description:    "",
			labels:         map[string]string{},
			expectedLabels: map[string]string{},
			wantErr:        false,
		},
		"Description not formatted as json should return an error": {
			description:    "test",
			labels:         map[string]string{},
			expectedLabels: map[string]string{},
			wantErr:        true,
		},
		"Description formatted as json should return a map": {
			description:    `{"test": "test"}`,
			labels:         map[string]string{},
			expectedLabels: map[string]string{"test": "test"},
			wantErr:        false,
		},
		"Description formatted as json with multiple keys should return a map": {
			description: `{"kubernetes.io/created-for/pv/name":"pvc-32613356-4cee-481d-902f-daa7223d14ab","kubernetes.io/created-for/pvc/name":"prometheus-server-data-prometheus-0","kubernetes.io/created-for/pvc/namespace":"prometheus"}`,
			labels:      map[string]string{},
			expectedLabels: map[string]string{
				"kubernetes.io/created-for/pv/name":       "pvc-32613356-4cee-481d-902f-daa7223d14ab",
				"kubernetes.io/created-for/pvc/name":      "prometheus-server-data-prometheus-0",
				"kubernetes.io/created-for/pvc/namespace": "prometheus",
			},
			wantErr: false,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if err := extractLabelsFromDesc(tt.description, tt.labels); (err != nil) != tt.wantErr {
				t.Errorf("extractLabelsFromDesc() error = %v, wantErr %v", err, tt.wantErr)
			}
			require.Equal(t, tt.expectedLabels, tt.labels)
		})
	}
}

func Test_getNamespaceFromDisk(t *testing.T) {
	tests := map[string]struct {
		disk *computev1.Disk
		want string
	}{
		"Empty description should return an empty string": {
			disk: &computev1.Disk{
				Description: "",
			},
			want: "",
		},
		"Description not formatted as json should return an empty string": {
			disk: &computev1.Disk{
				Description: "test",
			},
			want: "",
		},
		"Description formatted as json with multiple keys should return a namespace": {
			disk: &computev1.Disk{
				Description: `{"kubernetes.io/created-for/pv/name":"pvc-32613356-4cee-481d-902f-daa7223d14ab","kubernetes.io/created-for/pvc/name":"prometheus","kubernetes.io/created-for/pvc/namespace":"prometheus"}`,
			},
			want: "prometheus",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := getNamespaceFromDisk(tt.disk); got != tt.want {
				t.Errorf("getNamespaceFromDisk() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_getRegionFromDisk(t *testing.T) {
	tests := map[string]struct {
		disk *computev1.Disk
		want string
	}{
		"Empty zone should return an empty string": {
			disk: &computev1.Disk{
				Zone: "",
			},
			want: "",
		},
		"Zone formatted as a path should return the region": {
			disk: &computev1.Disk{
				Zone: "projects/123/zones/us-central1-a",
			},
			want: "us-central1",
		},
		"Disk with zone as label should return the region parsed properly": {
			disk: &computev1.Disk{
				Labels: map[string]string{
					compute.GkeRegionLabel: "us-central1-f",
				},
			},
			want: "us-central1",
		},
		"Disk with a label doesn't belong to a specific zone should return the full label": {
			disk: &computev1.Disk{
				Labels: map[string]string{
					compute.GkeRegionLabel: "us-central1",
				},
			},
			want: "us-central1",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := getRegionFromDisk(tt.disk); got != tt.want {
				t.Errorf("getRegionFromDisk() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_getNameFromDisk(t *testing.T) {
	tests := map[string]struct {
		disk *computev1.Disk
		want string
	}{
		"Empty description should return an empty string": {
			disk: &computev1.Disk{
				Description: "",
			},
			want: "",
		},
		"Description not formatted as json should return an empty string": {
			disk: &computev1.Disk{
				Description: "test",
			},
			want: "",
		},
		"Description not formatted as json should return the disks name": {
			disk: &computev1.Disk{
				Description: "test",
			},
			want: "",
		},
		"Description formatted as json with multiple keys should return the name": {
			disk: &computev1.Disk{
				Description: `{"kubernetes.io/created-for/pv/name":"pvc-32613356-4cee-481d-902f-daa7223d14ab","kubernetes.io/created-for/pvc/name":"prometheus","kubernetes.io/created-for/pvc/namespace":"prometheus"}`,
			},
			want: "pvc-32613356-4cee-481d-902f-daa7223d14ab",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := getNameFromDisk(tt.disk); got != tt.want {
				t.Errorf("getNameFromDisk() = %v, want %v", got, tt.want)
			}
		})
	}
}
