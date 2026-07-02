package gke

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	billingv1 "cloud.google.com/go/billing/apiv1"
	"cloud.google.com/go/billing/apiv1/billingpb"
	"github.com/grafana/cloudcost-exporter/pkg/google/client"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
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
		expectedMetrics []*utils.MetricResult
	}{
		"Handle http error": {
			config: &Config{
				Projects: "testing",
			},
			testServer: httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			})),
			expectedMetrics: []*utils.MetricResult{},
		},
		"Parse our regular response": {
			config: &Config{
				Projects: "testing,testing-1",
			},
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

			gcpClient := client.NewMock("testing", 0, nil, nil, cloudCatalogClient, computeService, nil, nil)
			collector, err := New(t.Context(), test.config, logger, gcpClient)
			require.NoError(t, err)
			require.NotNil(t, collector)

			// Wait for background stores to complete their initial population before collecting.
			<-collector.nodeStore.Done()
			<-collector.diskStore.Done()

			ch := make(chan prometheus.Metric)
			go func() {
				require.NoError(t, collector.Collect(t.Context(), ch))
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

// concurrentGCPClient tracks the peak number of goroutines running
// ListInstancesInZone and ListDisks simultaneously during NodeStore.Populate
// and DiskStore.Populate.
type concurrentGCPClient struct {
	client.Client

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

func testPopulateErrorsCounter() *prometheus.CounterVec {
	return newPopulateErrorsCounter()
}

// newSeededNodeStore returns a NodeStore without starting a populate goroutine.
// Passing nil for lgr uses the package-default logger. The helper always applies
// the same .With("store", "nodes") wrapping as NewNodeStore so log-content
// assertions exercise that attribute.
func newSeededNodeStore(t *testing.T, lgr *slog.Logger, gcpClient client.Client, projects []string, seed map[string]map[string][]*client.MachineSpec, counter *prometheus.CounterVec) *NodeStore {
	t.Helper()
	if lgr == nil {
		lgr = logger
	}
	if seed == nil {
		seed = make(map[string]map[string][]*client.MachineSpec)
	}
	if counter == nil {
		counter = newPopulateErrorsCounter()
	}
	return &NodeStore{
		logger:            lgr.With("store", "nodes"),
		gcpClient:         gcpClient,
		projects:          projects,
		concurrency:       DefaultZoneCollectConcurrency,
		populateErrors:    counter,
		nodes:             seed,
		initialPopulation: make(chan struct{}),
	}
}

func newSeededDiskStore(t *testing.T, lgr *slog.Logger, gcpClient client.Client, projects []string, seed map[string]map[string][]*Disk, counter *prometheus.CounterVec) *DiskStore {
	t.Helper()
	if lgr == nil {
		lgr = logger
	}
	if seed == nil {
		seed = make(map[string]map[string][]*Disk)
	}
	if counter == nil {
		counter = newPopulateErrorsCounter()
	}
	return &DiskStore{
		logger:            lgr.With("store", "disks"),
		gcpClient:         gcpClient,
		projects:          projects,
		concurrency:       DefaultZoneCollectConcurrency,
		populateErrors:    counter,
		disks:             seed,
		initialPopulation: make(chan struct{}),
	}
}

func TestNodeStore_Populate_ConcurrencyLimit(t *testing.T) {
	// Zones > limit so the cap is actually exercised.
	const numZones = DefaultZoneCollectConcurrency + 3

	zones := make([]*computev1.Zone, numZones)
	for i := range numZones {
		zones[i] = &computev1.Zone{Name: fmt.Sprintf("us-central1-%c", 'a'+i)}
	}

	fakeClient := &concurrentGCPClient{zones: zones}
	ns := newSeededNodeStore(t, nil, fakeClient, []string{"proj1"}, nil, nil)

	ns.Populate(t.Context())

	assert.LessOrEqual(t, fakeClient.peakConcurrency, DefaultZoneCollectConcurrency,
		"peak goroutine concurrency must not exceed DefaultZoneCollectConcurrency")
}

func TestDiskStore_Populate_ConcurrencyLimit(t *testing.T) {
	// Zones > limit so the cap is actually exercised.
	const numZones = DefaultZoneCollectConcurrency + 3

	zones := make([]*computev1.Zone, numZones)
	for i := range numZones {
		zones[i] = &computev1.Zone{Name: fmt.Sprintf("us-central1-%c", 'a'+i)}
	}

	fakeClient := &concurrentGCPClient{zones: zones}
	ds := newSeededDiskStore(t, nil, fakeClient, []string{"proj1"}, nil, nil)

	ds.Populate(t.Context())

	assert.LessOrEqual(t, fakeClient.peakConcurrency, DefaultZoneCollectConcurrency,
		"peak goroutine concurrency must not exceed DefaultZoneCollectConcurrency")
}

// Guards the gke.Config.ZoneConcurrency plumbing: an explicit value passed to
// NewNodeStore must cap the populate concurrency, not the default.
func TestNewNodeStore_HonorsConcurrencyArg(t *testing.T) {
	const customLimit = 3
	const numZones = customLimit + 5

	zones := make([]*computev1.Zone, numZones)
	for i := range numZones {
		zones[i] = &computev1.Zone{Name: fmt.Sprintf("us-central1-%d", i)}
	}

	fakeClient := &concurrentGCPClient{zones: zones}
	ns := NewNodeStore(t.Context(), logger, fakeClient, []string{"proj1"}, customLimit, testPopulateErrorsCounter())
	<-ns.Done()

	assert.LessOrEqual(t, fakeClient.peakConcurrency, customLimit,
		"peak goroutine concurrency must not exceed the supplied concurrency arg")
}

// Same guard for NewDiskStore.
func TestNewDiskStore_HonorsConcurrencyArg(t *testing.T) {
	const customLimit = 3
	const numZones = customLimit + 5

	zones := make([]*computev1.Zone, numZones)
	for i := range numZones {
		zones[i] = &computev1.Zone{Name: fmt.Sprintf("us-central1-%d", i)}
	}

	fakeClient := &concurrentGCPClient{zones: zones}
	ds := NewDiskStore(t.Context(), logger, fakeClient, []string{"proj1"}, customLimit, testPopulateErrorsCounter())
	<-ds.Done()

	assert.LessOrEqual(t, fakeClient.peakConcurrency, customLimit,
		"peak goroutine concurrency must not exceed the supplied concurrency arg")
}

func TestCollector_Collect_EmitsBothNodeAndDiskMetrics(t *testing.T) {
	const (
		numNodes = 10
		numDisks = 10
		region   = "us-central1"
		project  = "p1"
	)

	pm := &PricingMap{
		compute: map[string]*FamilyPricing{
			region: {Family: map[string]*PriceTiers{
				"n1": {OnDemand: Prices{Cpu: 1, Ram: 1}},
			}},
		},
		storage: map[string]*StoragePricing{
			region: {Storage: map[string]*StoragePrices{
				"pd-standard": {ProvisionedSpaceGiB: 1},
			}},
		},
	}

	nodes := make([]*client.MachineSpec, numNodes)
	for i := range numNodes {
		nodes[i] = &client.MachineSpec{
			Instance:    fmt.Sprintf("node-%d", i),
			Region:      region,
			Family:      "n1",
			MachineType: "n1-slim",
			PriceTier:   "ondemand",
			Labels:      map[string]string{client.GkeClusterLabel: "cluster1"},
		}
	}
	nodeStore := newSeededNodeStore(t, nil, nil, []string{project}, map[string]map[string][]*client.MachineSpec{project: {"zone-a": nodes}}, nil)
	close(nodeStore.initialPopulation)

	disks := make([]*Disk, numDisks)
	for i := range numDisks {
		disks[i] = NewDisk(&computev1.Disk{
			Name:   fmt.Sprintf("disk-%d", i),
			Zone:   "projects/p/zones/us-central1-a",
			Labels: map[string]string{client.GkeRegionLabel: region},
			Type:   "projects/p/zones/us-central1-a/diskTypes/pd-standard",
			SizeGb: 10,
		}, project)
	}
	diskStore := newSeededDiskStore(t, nil, nil, []string{project}, map[string]map[string][]*Disk{project: {"zone-a": disks}}, nil)
	close(diskStore.initialPopulation)

	collector := &Collector{
		projects:   []string{project},
		pricingMap: pm,
		nodeStore:  nodeStore,
		diskStore:  diskStore,
		logger:     logger,
	}

	const totalNodeMetrics = numNodes * 2
	const totalDiskMetrics = numDisks
	ch := make(chan prometheus.Metric, totalNodeMetrics+totalDiskMetrics)
	require.NoError(t, collector.Collect(t.Context(), ch))
	close(ch)

	var nodeCount, diskCount int
	for m := range ch {
		if strings.Contains(m.Desc().String(), "persistent_volume") {
			diskCount++
		} else {
			nodeCount++
		}
	}
	assert.Equal(t, totalNodeMetrics, nodeCount, "expected %d node metrics", totalNodeMetrics)
	assert.Equal(t, totalDiskMetrics, diskCount, "expected %d disk metrics", totalDiskMetrics)
}

// programmableGCPClient returns configurable per-zone results for testing
// partial- and total-failure paths.
type programmableGCPClient struct {
	client.Client

	zones       []*computev1.Zone
	instances   map[string][]*client.MachineSpec // zone name → instances (nil + present in errs means error)
	disks       map[string][]*computev1.Disk     // zone name → disks
	errs        map[string]error                 // zone name → error to return
	getZonesErr error                            // returned by all GetZones calls when set
}

func (p *programmableGCPClient) GetZones(_ string) ([]*computev1.Zone, error) {
	if p.getZonesErr != nil {
		return nil, p.getZonesErr
	}
	return p.zones, nil
}

func (p *programmableGCPClient) ListInstancesInZone(_, zone string) ([]*client.MachineSpec, error) {
	if err, ok := p.errs[zone]; ok {
		return nil, err
	}
	return p.instances[zone], nil
}

func (p *programmableGCPClient) ListDisks(_ context.Context, _, zone string) ([]*computev1.Disk, error) {
	if err, ok := p.errs[zone]; ok {
		return nil, err
	}
	return p.disks[zone], nil
}

func TestNodeStore_Populate_PartialZoneFailure_KeepsSuccessfulZones(t *testing.T) {
	fake := &programmableGCPClient{
		zones: []*computev1.Zone{{Name: "zone-ok"}, {Name: "zone-bad"}},
		instances: map[string][]*client.MachineSpec{
			"zone-ok": {{Instance: "node-ok"}},
		},
		errs: map[string]error{"zone-bad": fmt.Errorf("transient gcp error")},
	}

	ns := newSeededNodeStore(t, nil, fake, []string{"p1"}, nil, nil)

	ns.Populate(t.Context())

	got := ns.GetNodes("p1")
	require.Len(t, got, 1, "expected the successful zone's data to be stored")
	assert.Equal(t, "node-ok", got[0].Instance)
}

func TestNodeStore_Populate_AllZonesFail_PreservesStaleCache(t *testing.T) {
	stale := []*client.MachineSpec{{Instance: "stale-node"}}

	fake := &programmableGCPClient{
		zones: []*computev1.Zone{{Name: "zone-a"}, {Name: "zone-b"}},
		errs: map[string]error{
			"zone-a": fmt.Errorf("transient gcp error"),
			"zone-b": fmt.Errorf("transient gcp error"),
		},
	}

	ns := newSeededNodeStore(t, nil, fake, []string{"p1"}, map[string]map[string][]*client.MachineSpec{"p1": {"stale-zone": stale}}, nil)

	ns.Populate(t.Context())

	got := ns.GetNodes("p1")
	require.Len(t, got, 1, "stale cache should be preserved when every zone fails")
	assert.Equal(t, "stale-node", got[0].Instance)
}

func TestDiskStore_Populate_PartialZoneFailure_KeepsSuccessfulZones(t *testing.T) {
	fake := &programmableGCPClient{
		zones: []*computev1.Zone{{Name: "zone-ok"}, {Name: "zone-bad"}},
		disks: map[string][]*computev1.Disk{
			"zone-ok": {{Name: "disk-ok"}},
		},
		errs: map[string]error{"zone-bad": fmt.Errorf("transient gcp error")},
	}

	ds := newSeededDiskStore(t, nil, fake, []string{"p1"}, nil, nil)

	ds.Populate(t.Context())

	got := ds.GetDisks("p1")
	require.Len(t, got, 1, "expected the successful zone's data to be stored")
	assert.Equal(t, "disk-ok", got[0].Name())
}

func TestDiskStore_Populate_AllZonesFail_PreservesStaleCache(t *testing.T) {
	stale := []*Disk{NewDisk(&computev1.Disk{Name: "stale-disk"}, "p1")}

	fake := &programmableGCPClient{
		zones: []*computev1.Zone{{Name: "zone-a"}, {Name: "zone-b"}},
		errs: map[string]error{
			"zone-a": fmt.Errorf("transient gcp error"),
			"zone-b": fmt.Errorf("transient gcp error"),
		},
	}

	ds := newSeededDiskStore(t, nil, fake, []string{"p1"}, map[string]map[string][]*Disk{"p1": {"stale-zone": stale}}, nil)

	ds.Populate(t.Context())

	got := ds.GetDisks("p1")
	require.Len(t, got, 1, "stale cache should be preserved when every zone fails")
	assert.Equal(t, "stale-disk", got[0].Name())
}

// blockingGCPClient blocks every list call until release is closed; saturation
// closes once limit calls are in flight, enabling deterministic ctx-cancel tests.
type blockingGCPClient struct {
	client.Client

	zones      []*computev1.Zone
	callCount  atomic.Int64
	saturation chan struct{}
	release    chan struct{}
	limit      int64
	returnErr  error
	once       sync.Once
}

func (b *blockingGCPClient) GetZones(_ string) ([]*computev1.Zone, error) {
	return b.zones, nil
}

func (b *blockingGCPClient) signalSaturation() {
	if b.callCount.Add(1) == b.limit {
		b.once.Do(func() { close(b.saturation) })
	}
}

func (b *blockingGCPClient) ListInstancesInZone(_, _ string) ([]*client.MachineSpec, error) {
	b.signalSaturation()
	<-b.release
	return nil, b.returnErr
}

func (b *blockingGCPClient) ListDisks(_ context.Context, _, _ string) ([]*computev1.Disk, error) {
	b.signalSaturation()
	<-b.release
	return nil, b.returnErr
}

func makeZones(n int) []*computev1.Zone {
	zones := make([]*computev1.Zone, n)
	for i := range n {
		zones[i] = &computev1.Zone{Name: fmt.Sprintf("zone-%d", i)}
	}
	return zones
}

func TestNodeStore_Populate_HonorsContextCancellation(t *testing.T) {
	// Zones > limit so iterations queue on sem; cancellation must unblock them.
	const numZones = DefaultZoneCollectConcurrency * 3

	fake := &blockingGCPClient{
		zones:      makeZones(numZones),
		saturation: make(chan struct{}),
		release:    make(chan struct{}),
		limit:      DefaultZoneCollectConcurrency,
	}

	ns := newSeededNodeStore(t, nil, fake, []string{"p1"}, nil, nil)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	populateDone := make(chan struct{})
	go func() {
		ns.Populate(ctx)
		close(populateDone)
	}()

	select {
	case <-fake.saturation:
	case <-time.After(2 * time.Second):
		close(fake.release)
		t.Fatal("timed out waiting for in-flight calls to saturate")
	}

	cancel()
	close(fake.release)

	select {
	case <-populateDone:
	case <-time.After(2 * time.Second):
		t.Fatal("Populate did not return after context cancellation")
	}

	assert.Equal(t, int64(DefaultZoneCollectConcurrency), fake.callCount.Load(),
		"context cancellation should prevent additional zone calls beyond the in-flight batch")
}

func TestDiskStore_Populate_HonorsContextCancellation(t *testing.T) {
	const numZones = DefaultZoneCollectConcurrency * 3

	fake := &blockingGCPClient{
		zones:      makeZones(numZones),
		saturation: make(chan struct{}),
		release:    make(chan struct{}),
		limit:      DefaultZoneCollectConcurrency,
	}

	ds := newSeededDiskStore(t, nil, fake, []string{"p1"}, nil, nil)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	populateDone := make(chan struct{})
	go func() {
		ds.Populate(ctx)
		close(populateDone)
	}()

	select {
	case <-fake.saturation:
	case <-time.After(2 * time.Second):
		close(fake.release)
		t.Fatal("timed out waiting for in-flight calls to saturate")
	}

	cancel()
	close(fake.release)

	select {
	case <-populateDone:
	case <-time.After(2 * time.Second):
		t.Fatal("Populate did not return after context cancellation")
	}

	assert.Equal(t, int64(DefaultZoneCollectConcurrency), fake.callCount.Load(),
		"context cancellation should prevent additional zone calls beyond the in-flight batch")
}

// A tick that fires while a previous populate is still running must be dropped,
// not stacked. Without the in-flight guard the two populates would race on
// ns.nodes and double the GCP API load.
func TestNodeStore_Populate_SkipsConcurrentInvocation(t *testing.T) {
	fake := &blockingGCPClient{
		zones:      makeZones(DefaultZoneCollectConcurrency * 2),
		saturation: make(chan struct{}),
		release:    make(chan struct{}),
		limit:      DefaultZoneCollectConcurrency,
	}

	var logBuf bytes.Buffer
	captureLogger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	ns := newSeededNodeStore(t, captureLogger, fake, []string{"p1"}, nil, nil)

	firstDone := make(chan struct{})
	go func() {
		ns.Populate(t.Context())
		close(firstDone)
	}()

	select {
	case <-fake.saturation:
	case <-time.After(2 * time.Second):
		close(fake.release)
		t.Fatal("timed out waiting for first populate to saturate")
	}

	secondDone := make(chan struct{})
	go func() {
		ns.Populate(t.Context())
		close(secondDone)
	}()
	select {
	case <-secondDone:
	case <-time.After(500 * time.Millisecond):
		close(fake.release)
		t.Fatal("overlapping populate did not return quickly; in-flight guard missing")
	}

	assert.Contains(t, logBuf.String(), "populate already in progress",
		"overlapping populate should log a skip")
	assert.Contains(t, logBuf.String(), `store=nodes`,
		"log must carry the store=nodes attribute applied by NewNodeStore")

	close(fake.release)
	<-firstDone
}

func TestDiskStore_Populate_SkipsConcurrentInvocation(t *testing.T) {
	fake := &blockingGCPClient{
		zones:      makeZones(DefaultZoneCollectConcurrency * 2),
		saturation: make(chan struct{}),
		release:    make(chan struct{}),
		limit:      DefaultZoneCollectConcurrency,
	}

	var logBuf bytes.Buffer
	captureLogger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	ds := newSeededDiskStore(t, captureLogger, fake, []string{"p1"}, nil, nil)

	firstDone := make(chan struct{})
	go func() {
		ds.Populate(t.Context())
		close(firstDone)
	}()

	select {
	case <-fake.saturation:
	case <-time.After(2 * time.Second):
		close(fake.release)
		t.Fatal("timed out waiting for first populate to saturate")
	}

	secondDone := make(chan struct{})
	go func() {
		ds.Populate(t.Context())
		close(secondDone)
	}()
	select {
	case <-secondDone:
	case <-time.After(500 * time.Millisecond):
		close(fake.release)
		t.Fatal("overlapping populate did not return quickly; in-flight guard missing")
	}

	assert.Contains(t, logBuf.String(), "populate already in progress",
		"overlapping populate should log a skip")
	assert.Contains(t, logBuf.String(), `store=disks`,
		"log must carry the store=disks attribute applied by NewDiskStore")

	close(fake.release)
	<-firstDone
}

func TestNodeStore_Populate_IncrementsCounter_OnGetZonesError(t *testing.T) {
	counter := testPopulateErrorsCounter()
	fake := &programmableGCPClient{getZonesErr: fmt.Errorf("zone api down")}
	ns := newSeededNodeStore(t, nil, fake, []string{"p1"}, nil, counter)

	ns.Populate(t.Context())

	assert.Equal(t, float64(1), testutil.ToFloat64(counter.WithLabelValues("nodes", "p1", "get_zones")))
}

func TestNodeStore_Populate_IncrementsCounter_OnListInstancesError(t *testing.T) {
	counter := testPopulateErrorsCounter()
	fake := &programmableGCPClient{
		zones: []*computev1.Zone{{Name: "zone-a"}},
		errs:  map[string]error{"zone-a": fmt.Errorf("instances api down")},
	}
	ns := newSeededNodeStore(t, nil, fake, []string{"p1"}, nil, counter)

	ns.Populate(t.Context())

	assert.Equal(t, float64(1), testutil.ToFloat64(counter.WithLabelValues("nodes", "p1", "list_instances")))
}

func TestDiskStore_Populate_IncrementsCounter_OnGetZonesError(t *testing.T) {
	counter := testPopulateErrorsCounter()
	fake := &programmableGCPClient{getZonesErr: fmt.Errorf("zone api down")}
	ds := newSeededDiskStore(t, nil, fake, []string{"p1"}, nil, counter)

	ds.Populate(t.Context())

	assert.Equal(t, float64(1), testutil.ToFloat64(counter.WithLabelValues("disks", "p1", "get_zones")))
}

func TestDiskStore_Populate_IncrementsCounter_OnListDisksError(t *testing.T) {
	counter := testPopulateErrorsCounter()
	fake := &programmableGCPClient{
		zones: []*computev1.Zone{{Name: "zone-a"}},
		errs:  map[string]error{"zone-a": fmt.Errorf("disks api down")},
	}
	ds := newSeededDiskStore(t, nil, fake, []string{"p1"}, nil, counter)

	ds.Populate(t.Context())

	assert.Equal(t, float64(1), testutil.ToFloat64(counter.WithLabelValues("disks", "p1", "list_disks")))
}
