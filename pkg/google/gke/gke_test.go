package gke

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	billingv1 "cloud.google.com/go/billing/apiv1"
	"cloud.google.com/go/billing/apiv1/billingpb"
	"github.com/grafana/cloudcost-exporter/pkg/google/client"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	computev1 "google.golang.org/api/compute/v1"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/grafana/cloudcost-exporter/pkg/utils"
)

var logger = slog.New(slog.NewTextHandler(os.Stdout, nil))

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
				Logger:   logger,
			},
			testServer: httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			})),
			err:             client.ErrListInstances,
			collectResponse: 0,
			expectedMetrics: []*utils.MetricResult{},
		},
		"Parse our regular response": {
			config: &Config{
				Projects: "testing,testing-1",

				Logger: logger,
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
					FqName: "cloudcost_gcp_gke_persistent_volume_usd_per_hour",
					Labels: map[string]string{
						"cluster_name":     "test",
						"namespace":        "cloudcost-exporter",
						"persistentvolume": "test-disk",
						"region":           "us-central1",
						"project":          "testing",
						"storage_class":    "pd-standard",
						"disk_type":        "boot_disk",
						"use_status":       inUseDisk,
					},
					Value:      0,
					MetricType: prometheus.GaugeValue,
				},
				{
					FqName: "cloudcost_gcp_gke_persistent_volume_usd_per_hour",
					Labels: map[string]string{
						"cluster_name":     "test",
						"namespace":        "cloudcost-exporter",
						"persistentvolume": "test-ssd-disk",
						"region":           "us-east4",
						"project":          "testing",
						"storage_class":    "pd-ssd",
						"disk_type":        "persistent_volume",
						"use_status":       idleDisk,
					},
					Value:      0.15359342915811086,
					MetricType: prometheus.GaugeValue,
				},
				{
					FqName: "cloudcost_gcp_gke_persistent_volume_usd_per_hour",
					Labels: map[string]string{
						"cluster_name":     "test",
						"namespace":        "cloudcost-exporter",
						"persistentvolume": "test-ssd-disk",
						"region":           "us-east4",
						"project":          "testing-1",
						"storage_class":    "pd-ssd",
						"disk_type":        "persistent_volume",
						"use_status":       idleDisk,
					},
					Value:      0.15359342915811086,
					MetricType: prometheus.GaugeValue,
				},
				{
					FqName: "cloudcost_gcp_gke_persistent_volume_usd_per_hour",
					Labels: map[string]string{
						"cluster_name":     "test",
						"namespace":        "cloudcost-exporter",
						"persistentvolume": "test-disk",
						"region":           "us-central1",
						"project":          "testing-1",
						"storage_class":    "pd-standard",
						"disk_type":        "boot_disk",
						"use_status":       inUseDisk,
					},
					Value:      0,
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
									client.GkeClusterLabel: "test",
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
									client.GkeClusterLabel: "test",
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
									client.GkeClusterLabel: "test",
								},
							},
							{
								// Add in an instance that does not have a machine type that would exist in the pricing map.
								// This test replicates and fixes https://github.com/grafana/cloudcost-exporter/issues/335
								Name:        "test-n1-spot",
								MachineType: "abc/n8-slim",
								Zone:        "testing/us-central1-a",
								Scheduling: &computev1.Scheduling{
									ProvisioningModel: "SPOT",
								},
								Labels: map[string]string{
									client.GkeClusterLabel: "test",
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
									client.GkeClusterLabel: "test",
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
									client.GkeClusterLabel: "test",
									BootDiskLabel:          "",
								},
								Description: `{"kubernetes.io/created-for/pvc/namespace":"cloudcost-exporter"}`,
								Type:        "pd-standard",
								Users:       []string{"node-1"},
							},
							{
								Name: "test-ssd-disk",
								Zone: "testing/us-east4",
								Labels: map[string]string{
									client.GkeClusterLabel: "test",
								},
								Description: `{"kubernetes.io/created-for/pvc/namespace":"cloudcost-exporter"}`,
								Type:        "pd-ssd",
								SizeGb:      600,
							},
							{
								// Introduce a duplicated disk to ensure the duplicate doesn't cause an issue emitting metrics
								Name: "test-ssd-disk",
								Zone: "testing/us-east4",
								Labels: map[string]string{
									client.GkeClusterLabel: "test",
									BootDiskLabel:          "",
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
			computeService, err := computev1.NewService(t.Context(), option.WithoutAuthentication(), option.WithEndpoint(test.testServer.URL))
			require.NoError(t, err)
			l, err := net.Listen("tcp", "localhost:0")
			require.NoError(t, err)
			gsrv := grpc.NewServer()
			defer gsrv.Stop()
			billingpb.RegisterCloudCatalogServer(gsrv, &client.FakeCloudCatalogServer{})
			go func() {
				if err := gsrv.Serve(l); err != nil {
					t.Errorf("Failed to serve: %v", err)
				}
			}()

			cloudCatalogClient, err := billingv1.NewCloudCatalogClient(t.Context(),
				option.WithEndpoint(l.Addr().String()),
				option.WithoutAuthentication(),
				option.WithGRPCDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())),
			)
			require.NoError(t, err)

			gcpClient := client.NewMock("testing", 0, nil, nil, cloudCatalogClient, computeService, nil)
			collector, _ := New(t.Context(), test.config, gcpClient)
			require.NotNil(t, collector)
			ch := make(chan prometheus.Metric)
			go func() {
				err := collector.Collect(t.Context(), ch)
				if (err != nil) == (test.collectResponse == 1) {
					t.Errorf("expected collectResponse=%v, got err=%v", test.collectResponse, err)
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
			assert.ElementsMatch(t, metrics, test.expectedMetrics)
		})
	}
}

// concurrentGCPClient is a test double for client.Client that tracks the
// peak number of goroutines running ListInstancesInZone and ListDisks
// simultaneously.  Methods not needed by Collector.Collect are inherited from
// the embedded interface (nil) and will panic if called unexpectedly.
type concurrentGCPClient struct {
	client.Client // nil for all non-overridden methods

	zones []*computev1.Zone

	mu                 sync.Mutex
	currentConcurrency int
	peakConcurrency    int
}

func (c *concurrentGCPClient) GetZones(_ string) ([]*computev1.Zone, error) {
	return c.zones, nil
}

func (c *concurrentGCPClient) ListInstancesInZone(_ string, _ string) ([]*client.MachineSpec, error) {
	c.mu.Lock()
	c.currentConcurrency++
	if c.currentConcurrency > c.peakConcurrency {
		c.peakConcurrency = c.currentConcurrency
	}
	c.mu.Unlock()

	time.Sleep(10 * time.Millisecond)

	c.mu.Lock()
	c.currentConcurrency--
	c.mu.Unlock()
	return nil, nil
}

func (c *concurrentGCPClient) ListDisks(_ context.Context, _ string, _ string) ([]*computev1.Disk, error) {
	c.mu.Lock()
	c.currentConcurrency++
	if c.currentConcurrency > c.peakConcurrency {
		c.peakConcurrency = c.currentConcurrency
	}
	c.mu.Unlock()

	time.Sleep(10 * time.Millisecond)

	c.mu.Lock()
	c.currentConcurrency--
	c.mu.Unlock()
	return nil, nil
}

func TestCollector_ZoneConcurrencyLimit(t *testing.T) {
	// 8 zones → 16 goroutines (2 per zone: ListInstancesInZone + ListDisks),
	// but zoneCollectConcurrencyLimit=10 caps the total.
	const numZones = 8

	zones := make([]*computev1.Zone, numZones)
	for i := range numZones {
		zones[i] = &computev1.Zone{Name: fmt.Sprintf("us-central1-%c", 'a'+i)}
	}

	fakeClient := &concurrentGCPClient{zones: zones}

	// Build the Collector directly to bypass the billing/pricing initialisation
	// that New() performs — it is irrelevant for a concurrency test.
	collector := &Collector{
		gcpClient: fakeClient,
		config:    &Config{Logger: logger},
		projects:  []string{"proj1"},
		pricingMap: &PricingMap{
			compute: map[string]*FamilyPricing{},
			storage: map[string]*StoragePricing{},
		},
		logger: logger,
	}

	ch := make(chan prometheus.Metric, numZones*10)
	err := collector.Collect(t.Context(), ch)
	close(ch)
	require.NoError(t, err)

	assert.LessOrEqual(t, fakeClient.peakConcurrency, zoneCollectConcurrencyLimit,
		"peak zone goroutine concurrency must not exceed zoneCollectConcurrencyLimit")
}
