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

func TestCollector_ZoneConcurrencyLimit(t *testing.T) {
	// 20 zones → 40 goroutines (2 per zone: ListInstancesInZone + ListDisks),
	// with ZoneConcurrency=10 capping the total.
	const numZones = 20
	const limit = 10

	zones := make([]*computev1.Zone, numZones)
	for i := range numZones {
		zones[i] = &computev1.Zone{Name: fmt.Sprintf("us-central1-%c", 'a'+i)}
	}

	fakeClient := &concurrentGCPClient{zones: zones}

	// Build the Collector directly to bypass the billing/pricing initialisation
	// that New() performs — it is irrelevant for a concurrency test.
	collector := &Collector{
		gcpClient: fakeClient,
		config:    &Config{ZoneConcurrency: limit},
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

	assert.LessOrEqual(t, fakeClient.peakConcurrency, limit,
		"peak zone goroutine concurrency must not exceed configured ZoneConcurrency")
}

func TestCollector_ZoneConcurrencyDefault(t *testing.T) {
	// Empty Config.ZoneConcurrency should fall back to DefaultZoneCollectConcurrency.
	const numZones = 40

	zones := make([]*computev1.Zone, numZones)
	for i := range numZones {
		zones[i] = &computev1.Zone{Name: fmt.Sprintf("us-central1-%d", i)}
	}

	fakeClient := &concurrentGCPClient{zones: zones}
	collector := &Collector{
		gcpClient: fakeClient,
		config:    &Config{},
		projects:  []string{"proj1"},
		pricingMap: &PricingMap{
			compute: map[string]*FamilyPricing{},
			storage: map[string]*StoragePricing{},
		},
		logger: logger,
	}

	ns.Populate(t.Context())

	assert.LessOrEqual(t, fakeClient.peakConcurrency, DefaultZoneCollectConcurrency,
		"peak zone goroutine concurrency must not exceed DefaultZoneCollectConcurrency")
}

func TestDiskStore_Populate_ConcurrencyLimit(t *testing.T) {
	// 13 zones → up to 13 goroutines for ListDisks during DiskStore.Populate.
	// diskPopulateConcurrencyLimit caps the total.
	const numZones = 13

	zones := make([]*computev1.Zone, numZones)
	for i := range numZones {
		zones[i] = &computev1.Zone{Name: fmt.Sprintf("us-central1-%c", 'a'+i)}
	}

	fakeClient := &concurrentGCPClient{zones: zones}
	ds := newSeededDiskStore(t, fakeClient, []string{"proj1"}, nil)

	ds.Populate(t.Context())

	assert.LessOrEqual(t, fakeClient.peakConcurrency, diskPopulateConcurrencyLimit,
		"peak goroutine concurrency must not exceed diskPopulateConcurrencyLimit")
}

// TestCollector_Collect_NodeAndDiskEmitInParallel asserts that node and disk
// metric emission run concurrently inside Collect().
//
// With sequential emission, all metrics of one kind arrive before any metric
// of the other. With parallel emission, both goroutines are runnable at the
// same time and Go's FIFO channel queue interleaves their sends on an
// unbuffered channel, so the receive order alternates almost immediately.
//
// The test asserts that the first contiguous run of a single metric kind is
// shorter than the total count of that kind — true under any reasonable
// scheduling for the parallel implementation, impossible for a sequential one.
func TestCollector_Collect_NodeAndDiskEmitInParallel(t *testing.T) {
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
	nodeStore := newSeededNodeStore(t, nil, []string{project}, map[string][]*client.MachineSpec{project: nodes})
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
	diskStore := newSeededDiskStore(t, nil, []string{project}, map[string][]*Disk{project: disks})
	close(diskStore.initialPopulation)

	collector := &Collector{
		projects:   []string{project},
		pricingMap: pm,
		nodeStore:  nodeStore,
		diskStore:  diskStore,
		logger:     logger,
	}

	ch := make(chan prometheus.Metric)
	go func() {
		require.NoError(t, collector.Collect(t.Context(), ch))
		close(ch)
	}()

	var order []string
	for m := range ch {
		if strings.Contains(m.Desc().String(), "persistent_volume") {
			order = append(order, "disk")
		} else {
			order = append(order, "node")
		}
	}

	const totalNodeMetrics = numNodes * 2
	const totalDiskMetrics = numDisks
	require.Len(t, order, totalNodeMetrics+totalDiskMetrics, "expected all metrics to be emitted")

	prefixLen := 1
	for i := 1; i < len(order); i++ {
		if order[i] != order[0] {
			break
		}
		prefixLen++
	}

	maxPrefix := totalNodeMetrics
	if order[0] == "disk" {
		maxPrefix = totalDiskMetrics
	}
	assert.Less(t, prefixLen, maxPrefix,
		"expected node and disk metrics to interleave (parallel emission), but got %d consecutive %s metrics at the start: sequential emission detected",
		prefixLen, order[0])
}

// programmableGCPClient is a fake gcp client whose per-zone results can be
// configured. Used to exercise partial- and total-failure paths in store
// populate logic.
type programmableGCPClient struct {
	client.Client

	zones     []*computev1.Zone
	instances map[string][]*client.MachineSpec // zone name → instances (nil + present in errs means error)
	disks     map[string][]*computev1.Disk     // zone name → disks
	errs      map[string]error                 // zone name → error to return
}

func (p *programmableGCPClient) GetZones(_ string) ([]*computev1.Zone, error) {
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

	ns := newSeededNodeStore(t, fake, []string{"p1"}, nil)

	ns.Populate(t.Context())

	got := ns.GetNodes("p1")
	require.Len(t, got, 1, "expected the successful zone's data to be stored")
	assert.Equal(t, "node-ok", got[0].Instance)
}

func TestNodeStore_Populate_AllZonesFail_LogsAndWipesCache(t *testing.T) {
	stale := []*client.MachineSpec{{Instance: "stale-node"}}

	fake := &programmableGCPClient{
		zones: []*computev1.Zone{{Name: "zone-a"}, {Name: "zone-b"}},
		errs: map[string]error{
			"zone-a": fmt.Errorf("transient gcp error"),
			"zone-b": fmt.Errorf("transient gcp error"),
		},
	}

	var logBuf bytes.Buffer
	ns := newSeededNodeStore(t, fake, []string{"p1"}, map[string][]*client.MachineSpec{"p1": stale})
	ns.logger = slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelError}))

	ns.Populate(t.Context())

	assert.Empty(t, ns.GetNodes("p1"), "cache should be wiped when every zone fails")
	assert.Contains(t, logBuf.String(), "all zone listings failed, wiping cached data",
		"expected an error log when every zone fails")
}

func TestDiskStore_Populate_PartialZoneFailure_KeepsSuccessfulZones(t *testing.T) {
	fake := &programmableGCPClient{
		zones: []*computev1.Zone{{Name: "zone-ok"}, {Name: "zone-bad"}},
		disks: map[string][]*computev1.Disk{
			"zone-ok": {{Name: "disk-ok"}},
		},
		errs: map[string]error{"zone-bad": fmt.Errorf("transient gcp error")},
	}

	ds := newSeededDiskStore(t, fake, []string{"p1"}, nil)

	ds.Populate(t.Context())

	got := ds.GetDisks("p1")
	require.Len(t, got, 1, "expected the successful zone's data to be stored")
	assert.Equal(t, "disk-ok", got[0].Name())
}

func TestDiskStore_Populate_AllZonesFail_LogsAndWipesCache(t *testing.T) {
	stale := []*Disk{NewDisk(&computev1.Disk{Name: "stale-disk"}, "p1")}

	fake := &programmableGCPClient{
		zones: []*computev1.Zone{{Name: "zone-a"}, {Name: "zone-b"}},
		errs: map[string]error{
			"zone-a": fmt.Errorf("transient gcp error"),
			"zone-b": fmt.Errorf("transient gcp error"),
		},
	}

	var logBuf bytes.Buffer
	ds := newSeededDiskStore(t, fake, []string{"p1"}, map[string][]*Disk{"p1": stale})
	ds.logger = slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelError}))

	ds.Populate(t.Context())

	assert.Empty(t, ds.GetDisks("p1"), "cache should be wiped when every zone fails")
	assert.Contains(t, logBuf.String(), "all zone listings failed, wiping cached data",
		"expected an error log when every zone fails")
}

// blockingGCPClient blocks every ListInstancesInZone / ListDisks call until
// release is closed. It signals via saturation once `limit` calls are in flight,
// letting tests deterministically cancel context after the concurrency pool is
// full.
type blockingGCPClient struct {
	client.Client

	zones      []*computev1.Zone
	callCount  atomic.Int64
	saturation chan struct{}
	release    chan struct{}
	limit      int64
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
	return nil, nil
}

func (b *blockingGCPClient) ListDisks(_ context.Context, _, _ string) ([]*computev1.Disk, error) {
	b.signalSaturation()
	<-b.release
	return nil, nil
}

// makeZones builds n distinct compute.Zone fixtures for use as GetZones returns.
func makeZones(n int) []*computev1.Zone {
	zones := make([]*computev1.Zone, n)
	for i := range n {
		zones[i] = &computev1.Zone{Name: fmt.Sprintf("zone-%d", i)}
	}
	return zones
}

func TestNodeStore_Populate_HonorsContextCancellation(t *testing.T) {
	// More zones than the concurrency limit so the for-loop blocks on sem
	// acquisition for iterations beyond nodePopulateConcurrencyLimit. We then
	// cancel ctx; without ctx-aware sem acquisition those queued iterations
	// would eventually fire when the in-flight batch drains.
	const numZones = nodePopulateConcurrencyLimit * 3

	fake := &blockingGCPClient{
		zones:      makeZones(numZones),
		saturation: make(chan struct{}),
		release:    make(chan struct{}),
		limit:      nodePopulateConcurrencyLimit,
	}

	ns := newSeededNodeStore(t, fake, []string{"p1"}, nil)
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

	assert.Equal(t, int64(nodePopulateConcurrencyLimit), fake.callCount.Load(),
		"context cancellation should prevent additional zone calls beyond the in-flight batch")
}

func TestDiskStore_Populate_HonorsContextCancellation(t *testing.T) {
	const numZones = diskPopulateConcurrencyLimit * 3

	fake := &blockingGCPClient{
		zones:      makeZones(numZones),
		saturation: make(chan struct{}),
		release:    make(chan struct{}),
		limit:      diskPopulateConcurrencyLimit,
	}

	ds := newSeededDiskStore(t, fake, []string{"p1"}, nil)
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

	assert.Equal(t, int64(diskPopulateConcurrencyLimit), fake.callCount.Load(),
		"context cancellation should prevent additional zone calls beyond the in-flight batch")
}
