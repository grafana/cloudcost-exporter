package compute

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	billingv1 "cloud.google.com/go/billing/apiv1"
	"cloud.google.com/go/billing/apiv1/billingpb"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/compute/v1"
	computev1 "google.golang.org/api/compute/v1"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/grafana/cloudcost-exporter/pkg/google/billing"
	"github.com/grafana/cloudcost-exporter/pkg/utils"
)

var collector *Collector

func TestMain(m *testing.M) {
	ctx := context.Background()

	computeService, err := compute.NewService(ctx)
	if err != nil {
		// We silently fail here so that CI works.
		// TODO Configure tests so the container gets application credentials by default
		log.Printf("Error creating compute computeService: %s", err)
	}

	billingService, err := billingv1.NewCloudCatalogClient(ctx)
	if err != nil {
		// We silently fail here so that CI works.
		// TODO Configure tests so the container gets application credentials by default
		log.Printf("Error creating billing billingService: %s", err)
	}
	collector = New(&Config{
		Projects: "some_project",
	}, computeService, billingService)
	code := m.Run()
	os.Exit(code)
}

func TestNewMachineSpec(t *testing.T) {
	tests := map[string]struct {
		instance *compute.Instance
		want     *MachineSpec
	}{
		"basic instance": {
			instance: &compute.Instance{
				Name:        "test",
				MachineType: "abc/abc-def",
				Zone:        "testing/abc-123",
				Scheduling: &compute.Scheduling{
					ProvisioningModel: "test",
				},
			},
			want: &MachineSpec{
				Instance:     "test",
				Zone:         "abc-123",
				Region:       "abc",
				MachineType:  "abc-def",
				Family:       "abc",
				SpotInstance: false,
				PriceTier:    "ondemand",
			},
		},
		"machine type with no value": {
			instance: &compute.Instance{
				Name:        "test",
				MachineType: "abc/",
				Zone:        "testing/abc-123",
				Scheduling: &compute.Scheduling{
					ProvisioningModel: "test",
				},
			},
			want: &MachineSpec{
				Instance:     "test",
				Zone:         "abc-123",
				Region:       "abc",
				MachineType:  "",
				Family:       "",
				SpotInstance: false,
				PriceTier:    "ondemand",
			},
		},
		"spot instance": {
			instance: &compute.Instance{
				Name:        "test",
				MachineType: "abc/abc-def",
				Zone:        "testing/abc-123",
				Scheduling: &compute.Scheduling{
					ProvisioningModel: "SPOT",
				},
			},
			want: &MachineSpec{
				Instance:     "test",
				Zone:         "abc-123",
				Region:       "abc",
				MachineType:  "abc-def",
				Family:       "abc",
				SpotInstance: true,
				PriceTier:    "spot",
			},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			got := NewMachineSpec(test.instance)
			require.Equal(t, got, test.want)
		})
	}
}

// note: this has been fixed in go 1.22, golangci-lint needs to catch up :)
//
//nolint:govet
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
			err:             ListInstancesError,
			collectResponse: 0,
			expectedMetrics: []*utils.MetricResult{},
		},
		"Parse out regular response": {
			config: &Config{
				Projects: "testing,testing-1",
			},
			collectResponse: 1.0,
			expectedMetrics: []*utils.MetricResult{
				{
					FqName: "cloudcost_gcp_compute_instance_cpu_usd_per_core_hour",
					Labels: map[string]string{
						"family":       "n1",
						"instance":     "test-n1",
						"machine_type": "n1-slim",
						"price_tier":   "ondemand",
						"project":      "testing",
						"region":       "us-central1",
					},
					Value:      1,
					MetricType: prometheus.GaugeValue,
				},
				{
					FqName: "cloudcost_gcp_compute_instance_ram_usd_per_gib_hour",
					Labels: map[string]string{
						"family":       "n1",
						"instance":     "test-n1",
						"machine_type": "n1-slim",
						"price_tier":   "ondemand",
						"project":      "testing",
						"region":       "us-central1",
					},
					Value:      1,
					MetricType: prometheus.GaugeValue,
				},
				{
					FqName: "cloudcost_gcp_compute_instance_cpu_usd_per_core_hour",
					Labels: map[string]string{
						"family":       "n2",
						"instance":     "test-n2",
						"machine_type": "n2-slim",
						"price_tier":   "ondemand",
						"project":      "testing",
						"region":       "us-central1",
					},
					Value:      1,
					MetricType: prometheus.GaugeValue,
				},
				{
					FqName: "cloudcost_gcp_compute_instance_ram_usd_per_gib_hour",
					Labels: map[string]string{
						"family":       "n2",
						"instance":     "test-n2",
						"machine_type": "n2-slim",
						"price_tier":   "ondemand",
						"project":      "testing",
						"region":       "us-central1",
					},
					Value:      1,
					MetricType: prometheus.GaugeValue,
				},
				{
					FqName: "cloudcost_gcp_compute_instance_cpu_usd_per_core_hour",
					Labels: map[string]string{
						"family":       "n1",
						"instance":     "test-n1-spot",
						"machine_type": "n1-slim",
						"price_tier":   "spot",
						"project":      "testing",
						"region":       "us-central1",
					},
					Value:      1,
					MetricType: prometheus.GaugeValue,
				},
				{
					FqName: "cloudcost_gcp_compute_instance_ram_usd_per_gib_hour",
					Labels: map[string]string{
						"family":       "n1",
						"instance":     "test-n1-spot",
						"machine_type": "n1-slim",
						"price_tier":   "spot",
						"project":      "testing",
						"region":       "us-central1",
					},
					Value:      1,
					MetricType: prometheus.GaugeValue,
				},
				{
					FqName: "cloudcost_gcp_compute_instance_cpu_usd_per_core_hour",
					Labels: map[string]string{
						"family":       "n2",
						"instance":     "test-n2-us-east1",
						"machine_type": "n2-slim",
						"price_tier":   "ondemand",
						"project":      "testing",
						"region":       "us-east1",
					},
					Value:      1,
					MetricType: prometheus.GaugeValue,
				},
				{
					FqName: "cloudcost_gcp_compute_instance_ram_usd_per_gib_hour",
					Labels: map[string]string{
						"family":       "n2",
						"instance":     "test-n2-us-east1",
						"machine_type": "n2-slim",
						"price_tier":   "ondemand",
						"project":      "testing",
						"region":       "us-east1",
					},
					Value:      1,
					MetricType: prometheus.GaugeValue,
				},
				{
					FqName: "cloudcost_gcp_compute_instance_cpu_usd_per_core_hour",
					Labels: map[string]string{
						"family":       "n1",
						"instance":     "test-n1",
						"machine_type": "n1-slim",
						"price_tier":   "ondemand",
						"project":      "testing-1",
						"region":       "us-central1",
					},
					Value:      1,
					MetricType: prometheus.GaugeValue,
				},
				{
					FqName: "cloudcost_gcp_compute_instance_ram_usd_per_gib_hour",
					Labels: map[string]string{
						"family":       "n1",
						"instance":     "test-n1",
						"machine_type": "n1-slim",
						"price_tier":   "ondemand",
						"project":      "testing-1",
						"region":       "us-central1",
					},
					Value:      1,
					MetricType: prometheus.GaugeValue,
				},
				{
					FqName: "cloudcost_gcp_compute_instance_cpu_usd_per_core_hour",
					Labels: map[string]string{
						"family":       "n2",
						"instance":     "test-n2",
						"machine_type": "n2-slim",
						"price_tier":   "ondemand",
						"project":      "testing-1",
						"region":       "us-central1",
					},
					Value:      1,
					MetricType: prometheus.GaugeValue,
				},
				{
					FqName: "cloudcost_gcp_compute_instance_ram_usd_per_gib_hour",
					Labels: map[string]string{
						"family":       "n2",
						"instance":     "test-n2",
						"machine_type": "n2-slim",
						"price_tier":   "ondemand",
						"project":      "testing-1",
						"region":       "us-central1",
					},
					Value:      1,
					MetricType: prometheus.GaugeValue,
				},
				{
					FqName: "cloudcost_gcp_compute_instance_cpu_usd_per_core_hour",
					Labels: map[string]string{
						"family":       "n1",
						"instance":     "test-n1-spot",
						"machine_type": "n1-slim",
						"price_tier":   "spot",
						"project":      "testing-1",
						"region":       "us-central1",
					},
					Value:      1,
					MetricType: prometheus.GaugeValue,
				},
				{
					FqName: "cloudcost_gcp_compute_instance_ram_usd_per_gib_hour",
					Labels: map[string]string{
						"family":       "n1",
						"instance":     "test-n1-spot",
						"machine_type": "n1-slim",
						"price_tier":   "spot",
						"project":      "testing-1",
						"region":       "us-central1",
					},
					Value:      1,
					MetricType: prometheus.GaugeValue,
				},
				{
					FqName: "cloudcost_gcp_compute_instance_cpu_usd_per_core_hour",
					Labels: map[string]string{
						"family":       "n2",
						"instance":     "test-n2-us-east1",
						"machine_type": "n2-slim",
						"price_tier":   "ondemand",
						"project":      "testing-1",
						"region":       "us-east1",
					},
					Value:      1,
					MetricType: prometheus.GaugeValue,
				},
				{
					FqName: "cloudcost_gcp_compute_instance_ram_usd_per_gib_hour",
					Labels: map[string]string{
						"family":       "n2",
						"instance":     "test-n2-us-east1",
						"machine_type": "n2-slim",
						"price_tier":   "ondemand",
						"project":      "testing-1",
						"region":       "us-east1",
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
							},
							{
								Name:        "test-n2",
								MachineType: "abc/n2-slim",
								Zone:        "testing/us-central1-a",
								Scheduling: &computev1.Scheduling{
									ProvisioningModel: "test",
								},
							},
							{
								Name:        "test-n1-spot",
								MachineType: "abc/n1-slim",
								Zone:        "testing/us-central1-a",
								Scheduling: &computev1.Scheduling{
									ProvisioningModel: "SPOT",
								},
							},
							{
								Name:        "test-n2-us-east1",
								MachineType: "abc/n2-slim",
								Zone:        "testing/us-east1-a",
								Scheduling: &computev1.Scheduling{
									ProvisioningModel: "test",
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
					t.Errorf("failed to serve: %v", err)
				}
			}()

			billingpb.RegisterCloudCatalogServer(gsrv, &billing.FakeCloudCatalogServer{})
			cloudCatalogClient, _ := billingv1.NewCloudCatalogClient(context.Background(),
				option.WithEndpoint(l.Addr().String()),
				option.WithoutAuthentication(),
				option.WithGRPCDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())),
			)

			collector := New(test.config, computeService, cloudCatalogClient)

			require.NotNil(t, collector)

			ch := make(chan prometheus.Metric)
			go func() {
				if up := collector.CollectMetrics(ch); up != test.collectResponse {
					t.Errorf("Expected up to be %f, but got %f", test.collectResponse, up)
				}
				close(ch)
			}()

			for _, expectedMetric := range test.expectedMetrics {
				m := utils.ReadMetrics(<-ch)
				if strings.Contains(m.FqName, "next_scrape") {
					// We don't have a great way right now of mocking out the time, so we just skip this metric and read the next available metric
					m = utils.ReadMetrics(<-ch)
				}
				require.Equal(t, expectedMetric, m)
			}
		})
	}
}

func TestCollector_GetPricing(t *testing.T) {
	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
					},
					{
						Name:        "test-n2",
						MachineType: "abc/n2-slim",
						Zone:        "testing/us-central1-a",
						Scheduling: &computev1.Scheduling{
							ProvisioningModel: "test",
						},
					},
					{
						Name:        "test-n1-spot",
						MachineType: "abc/n1-slim",
						Zone:        "testing/us-central1-a",
						Scheduling: &computev1.Scheduling{
							ProvisioningModel: "SPOT",
						},
					},
					{
						Name:        "test-n2-us-east1",
						MachineType: "abc/n2-slim",
						Zone:        "testing/us-east1-a",
						Scheduling: &computev1.Scheduling{
							ProvisioningModel: "test",
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
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(buf)
	}))

	computeService, err := computev1.NewService(context.Background(), option.WithoutAuthentication(), option.WithEndpoint(testServer.URL))
	require.NoError(t, err)
	// Create the collector with a nil billing service so we can override it on each test case
	collector := New(&Config{
		Projects: "testing",
	}, computeService, nil)

	var pricingMap *StructuredPricingMap
	t.Run("Test that the pricing map is cached", func(t *testing.T) {
		l, err := net.Listen("tcp", "localhost:0")
		require.NoError(t, err)
		gsrv := grpc.NewServer()
		defer gsrv.Stop()
		go func() {
			if err := gsrv.Serve(l); err != nil {
				t.Errorf("failed to serve: %v", err)
			}
		}()

		billingpb.RegisterCloudCatalogServer(gsrv, &billing.FakeCloudCatalogServer{})
		cloudCatalagClient, _ := billingv1.NewCloudCatalogClient(context.Background(),
			option.WithEndpoint(l.Addr().String()),
			option.WithoutAuthentication(),
			option.WithGRPCDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())),
		)

		collector.billingService = cloudCatalagClient

		require.NotNil(t, collector)

		ch := make(chan prometheus.Metric)
		defer close(ch)

		go func() {
			for range ch {
			}
		}()

		up := collector.CollectMetrics(ch)
		require.Equal(t, 1.0, up)

		pricingMap = collector.PricingMap
		up = collector.CollectMetrics(ch)
		require.Equal(t, 1.0, up)
		require.Equal(t, pricingMap, collector.PricingMap)
	})

	t.Run("Test that the pricing map is updated after the next scrape", func(t *testing.T) {
		l, err := net.Listen("tcp", "localhost:0")
		require.NoError(t, err)
		gsrv := grpc.NewServer()
		defer gsrv.Stop()
		go func() {
			if err := gsrv.Serve(l); err != nil {
				t.Errorf("failed to serve: %v", err)
			}
		}()
		billingpb.RegisterCloudCatalogServer(gsrv, &billing.FakeCloudCatalogServerSlimResults{})
		cloudCatalogClient, _ := billingv1.NewCloudCatalogClient(context.Background(),
			option.WithEndpoint(l.Addr().String()),
			option.WithoutAuthentication(),
			option.WithGRPCDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())),
		)

		collector.billingService = cloudCatalogClient
		collector.NextScrape = time.Now().Add(-1 * time.Minute)
		ch := make(chan prometheus.Metric)
		defer close(ch)
		go func() {
			for range ch {
			}
		}()
		up := collector.CollectMetrics(ch)
		require.Equal(t, 1.0, up)
		require.NotEqual(t, pricingMap, collector.PricingMap)
	})
}
