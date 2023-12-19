package compute

import (
	"context"
	"encoding/json"
	"fmt"
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
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/compute/v1"
	computev1 "google.golang.org/api/compute/v1"
	"google.golang.org/api/option"
	"google.golang.org/genproto/googleapis/type/money"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
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

func Test_stripOutKeyFromDescription(t *testing.T) {
	tests := map[string]struct {
		description string
		want        string
	}{
		"simple": {
			description: "N1 Predefined Instance Core running in Americas",
			want:        "N1 Predefined Instance Core",
		},
		"commitment v1: empty": {
			description: "Commitment v1:",
			want:        "",
		},
		"commitment v1": {
			description: "Commitment v1: N2 Predefined Instance Core in Americas",
			want:        "N2 Predefined Instance Core",
		},
		"commitment v2": {
			description: "Commitment v1: N2D AMD Ram in Americin for 1 year",
			want:        "N2D AMD Ram",
		},
		"commitment berlin": {
			description: "Commitment v1: G2 Ram in Berlin for 1 year",
			want:        "G2 Ram",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := stripOutKeyFromDescription(tt.description); got != tt.want {
				t.Errorf("stripOutKeyFromDescription() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_getMachineInfoFromMachineType(t *testing.T) {
	type result struct {
		wantCpu         int
		wantRam         int
		wantZone        string
		wantType        string
		wantMachineType string
	}
	tests := map[string]struct {
		machineType string
		want        result
	}{
		"simple": {
			machineType: "https://www.googleapis.com/compute/v1/projects/grafanalabs-dev/zones/us-central1-a/machineTypes/n2-standard-8",
			want: result{
				wantCpu:         2,
				wantRam:         8,
				wantZone:        "us-central1-a",
				wantMachineType: "n2-standard-8",
				wantType:        "n2",
			},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if got := getMachineTypeFromURL(test.machineType); got != test.want.wantMachineType {
				t.Errorf("getMachineTypeFromURL() = %v, want %v", got, test.want.wantMachineType)
			}
		})
	}
}

func Test_GetMachineFamily(t *testing.T) {
	tests := map[string]struct {
		machineType string
		want        string
	}{
		"n1": {
			machineType: "n1-standard-8",
			want:        "n1",
		},
		"n2": {
			machineType: "n2-standard-8",
			want:        "n2",
		},
		"n2-bad": {
			machineType: "n2_standard",
			want:        "",
		},
		"n2d": {
			machineType: "n2d-standard-8",
			want:        "n2d",
		},
		"e1": {
			machineType: "e2-standard-8",
			want:        "e2",
		},
		"g1": {
			machineType: "g1-standard-8",
			want:        "g1",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if got := getMachineFamily(test.machineType); got != test.want {
				t.Errorf("stripOutKeyFromDescription() = %v, want %v", got, test.want)
			}
		})
	}
}

// development tests: Following tests are meant to be run locally and not suited for CI
// If you need this tests for debugging purposes please run `TestGenerateTestFiles` first
// and then you can run the rest of tests as needed.

// enter here the project ID, where you want the collector be run.
var projectUnderTest = "<put project id here>"

func TestGenerateTestFiles(t *testing.T) {
	// todo: put this into a go gen step -> https://go.dev/blog/generate
	t.Skip("Test only to produce local test files. Comment this line to execute test.")
	serviceName, _ := collector.GetServiceName()
	skus := collector.GetPricing(serviceName)
	jsonData, err := json.Marshal(skus)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	file, err := os.OpenFile("testdata/all-products.json", os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		fmt.Println("Error opening file:", err)
		return
	}
	defer file.Close() // defer closing the file until the function exits

	// Write some data to the file
	_, err = file.Write(jsonData)
	if err != nil {
		fmt.Println("Error writing to file:", err)
		return
	}

	fmt.Println("Data written to file successfully.")
}

func Test_GetCostsOfInstances(t *testing.T) {
	t.Skip("Local only test. Comment this line to execute test.")
	instances, err := collector.ListInstances(projectUnderTest)
	if err != nil {
		t.Errorf("Error listing clusters: %s", err)
	}

	file, err := os.Open("testdata/all-products.json")
	if err != nil {
		fmt.Println("Error opening file:", err)
		return
	}
	defer file.Close() // defer closing the file until the function exits

	var pricing []*billingpb.Sku
	err = json.NewDecoder(file).Decode(&pricing)
	if err != nil {
		t.Errorf("Error decoding JSON: %s", err)
		return
	}
	pricingMap, err := GeneratePricingMap(pricing)
	if err != nil {
		t.Errorf("Error generating pricing map: %s", err)
	}
	for _, instance := range instances {
		cpuCost, ramCost, err := pricingMap.GetCostOfInstance(instance)
		if err != nil {
			fmt.Printf("%v: No costs found for this instance\n", instance.Instance)
		}
		fmt.Printf("%v: cpu hourly cost:%f ram hourly cost:%f \n", instance.Instance, cpuCost, ramCost)
	}
}

func TestGetPriceForOneMachine(t *testing.T) {
	t.Skip("Local only test. Comment this line to execute test.")
	instances, err := collector.ListInstances(projectUnderTest)
	file, err := os.Open("testdata/all-products.json")
	if err != nil {
		fmt.Printf("Error opening file: %s", err)
		return
	}
	defer file.Close() // defer closing the file until the function exits

	// Read the file into memory
	var pricing []*billingpb.Sku
	err = json.NewDecoder(file).Decode(&pricing)
	if err != nil {
		t.Errorf("Error decoding JSON: %s", err)
		return
	}
	pricingMap, err := GeneratePricingMap(pricing)
	if err != nil {
		t.Errorf("Error generating pricing map: %s", err)
	}
	fmt.Printf("%v,%v", instances, pricingMap)
}

func TestListInstances(t *testing.T) {
	t.Skip("Local only test. Comment this line to execute test.")
	instances, err := collector.ListInstances(projectUnderTest)
	if err != nil {
		t.Errorf("Error listing clusters: %s", err)
	}
	if len(instances) == 0 {
		t.Errorf("Expected at least one cluster, but got none")
	}
	for _, instance := range instances {
		fmt.Printf("%v:%s\n", instance.Instance, instance.Family)
	}
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

type fakeCloudCatalogServer struct {
	billingpb.UnimplementedCloudCatalogServer
}

func (s *fakeCloudCatalogServer) ListServices(ctx context.Context, req *billingpb.ListServicesRequest) (*billingpb.ListServicesResponse, error) {
	return &billingpb.ListServicesResponse{
		Services: []*billingpb.Service{
			{
				DisplayName: "Compute Engine",
				Name:        "compute-engine",
			},
		},
	}, nil
}

func (s *fakeCloudCatalogServer) ListSkus(ctx context.Context, req *billingpb.ListSkusRequest) (*billingpb.ListSkusResponse, error) {
	return &billingpb.ListSkusResponse{
		Skus: []*billingpb.Sku{
			{
				Name:           "test",
				Description:    "N1 Predefined Instance Core running in Americas",
				ServiceRegions: []string{"us-central1"},
				PricingInfo: []*billingpb.PricingInfo{
					{
						PricingExpression: &billingpb.PricingExpression{
							TieredRates: []*billingpb.PricingExpression_TierRate{
								{
									UnitPrice: &money.Money{
										CurrencyCode: "USD",
										Nanos:        1e9,
									},
								},
							},
						},
					},
				},
			},
			{
				Name:           "test2",
				Description:    "N1 Predefined Instance Ram running in Americas",
				ServiceRegions: []string{"us-central1"},
				PricingInfo: []*billingpb.PricingInfo{
					{
						PricingExpression: &billingpb.PricingExpression{
							TieredRates: []*billingpb.PricingExpression_TierRate{
								{
									UnitPrice: &money.Money{
										CurrencyCode: "USD",
										Nanos:        1e9,
									},
								},
							},
						},
					},
				},
			},
			{
				Name:           "test-spot",
				Description:    "Spot Preemptible N1 Instance Core running in Americas",
				ServiceRegions: []string{"us-central1"},
				PricingInfo: []*billingpb.PricingInfo{
					{
						PricingExpression: &billingpb.PricingExpression{
							TieredRates: []*billingpb.PricingExpression_TierRate{
								{
									UnitPrice: &money.Money{
										CurrencyCode: "USD",
										Nanos:        1e9,
									},
								},
							},
						},
					},
				},
			},
			{
				Name:           "test2-spot",
				Description:    "Spot Preemptible N1 Instance Ram running in Americas",
				ServiceRegions: []string{"us-central1"},
				PricingInfo: []*billingpb.PricingInfo{
					{
						PricingExpression: &billingpb.PricingExpression{
							TieredRates: []*billingpb.PricingExpression_TierRate{
								{
									UnitPrice: &money.Money{
										CurrencyCode: "USD",
										Nanos:        1e9,
									},
								},
							},
						},
					},
				},
			},
			{
				Name:           "test",
				Description:    "N2 Predefined Instance Core running in Americas",
				ServiceRegions: []string{"us-central1"},
				PricingInfo: []*billingpb.PricingInfo{
					{
						PricingExpression: &billingpb.PricingExpression{
							TieredRates: []*billingpb.PricingExpression_TierRate{
								{
									UnitPrice: &money.Money{
										CurrencyCode: "USD",
										Nanos:        1e9,
									},
								},
							},
						},
					},
				},
			},
			{
				Name:           "test2",
				Description:    "N2 Predefined Instance Ram running in Americas",
				ServiceRegions: []string{"us-central1"},
				PricingInfo: []*billingpb.PricingInfo{
					{
						PricingExpression: &billingpb.PricingExpression{
							TieredRates: []*billingpb.PricingExpression_TierRate{
								{
									UnitPrice: &money.Money{
										CurrencyCode: "USD",
										Nanos:        1e9,
									},
								},
							},
						},
					},
				},
			},
		},
	}, nil
}

type fakeCloudCatalogServerSlimResults struct {
	billingpb.UnimplementedCloudCatalogServer
}

func (s *fakeCloudCatalogServerSlimResults) ListServices(ctx context.Context, req *billingpb.ListServicesRequest) (*billingpb.ListServicesResponse, error) {
	return &billingpb.ListServicesResponse{
		Services: []*billingpb.Service{
			{
				DisplayName: "Compute Engine",
				Name:        "compute-engine",
			},
		},
	}, nil
}

func (s *fakeCloudCatalogServerSlimResults) ListSkus(ctx context.Context, req *billingpb.ListSkusRequest) (*billingpb.ListSkusResponse, error) {
	return &billingpb.ListSkusResponse{
		Skus: []*billingpb.Sku{
			{
				Name:           "test",
				Description:    "N1 Predefined Instance Core running in Americas",
				ServiceRegions: []string{"us-central1"},
				PricingInfo: []*billingpb.PricingInfo{
					{
						PricingExpression: &billingpb.PricingExpression{
							TieredRates: []*billingpb.PricingExpression_TierRate{
								{
									UnitPrice: &money.Money{
										CurrencyCode: "USD",
										Nanos:        1e9,
									},
								},
							},
						},
					},
				},
			},
		},
	}, nil
}

func TestCollector_Collect(t *testing.T) {
	tests := map[string]struct {
		config     *Config
		testServer *httptest.Server
		err        error
	}{
		"Handle http error": {
			config: &Config{
				Projects: "testing",
			},
			testServer: httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			})),
			err: ListInstancesError,
		},
		"Parse out regular response": {
			config: &Config{
				Projects: "testing,testing-1",
			},
			testServer: httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				buf := &compute.InstanceAggregatedList{
					Items: map[string]compute.InstancesScopedList{
						"projects/testing/zones/us-central1-a": {
							Instances: []*compute.Instance{
								{
									Name:        "test-n1",
									MachineType: "abc/n1-slim",
									Zone:        "testing/us-central1-a",
									Scheduling: &compute.Scheduling{
										ProvisioningModel: "test",
									},
								},
								{
									Name:        "test-n2",
									MachineType: "abc/n2-slim",
									Zone:        "testing/us-central1-a",
									Scheduling: &compute.Scheduling{
										ProvisioningModel: "test",
									},
								},
								{
									Name:        "test-n1-spot",
									MachineType: "abc/n1-slim",
									Zone:        "testing/us-central1-a",
									Scheduling: &compute.Scheduling{
										ProvisioningModel: "SPOT",
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

			billingpb.RegisterCloudCatalogServer(gsrv, &fakeCloudCatalogServer{})
			cloudCatalagClient, err := billingv1.NewCloudCatalogClient(context.Background(),
				option.WithEndpoint(l.Addr().String()),
				option.WithoutAuthentication(),
				option.WithGRPCDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())),
			)

			collector := New(test.config, computeService, cloudCatalagClient)

			require.NotNil(t, collector)

			err = collector.Collect()
			if test.err != nil {
				require.ErrorIs(t, err, test.err)
				return
			}

			r := prometheus.NewPedanticRegistry()
			err = collector.Register(r)
			assert.NoError(t, err)
			metricsNames := []string{
				"instance_cpu_hourly_cost",
				"instance_memory_hourly_cost",
			}
			err = testutil.CollectAndCompare(r, strings.NewReader(`
	# HELP instance_cpu_hourly_cost The hourly cost of a GKE instance
	# TYPE instance_cpu_hourly_cost gauge
	instance_cpu_hourly_cost{family="n1",instance="test-n1-spot",machine_type="n1-slim",price_tier="spot",project="testing",provider="gcp",region="us-central1"} 1
	instance_cpu_hourly_cost{family="n1",instance="test-n1-spot",machine_type="n1-slim",price_tier="spot",project="testing-1",provider="gcp",region="us-central1"} 1
	instance_cpu_hourly_cost{family="n1",instance="test-n1",machine_type="n1-slim",price_tier="ondemand",project="testing",provider="gcp",region="us-central1"} 1
	instance_cpu_hourly_cost{family="n1",instance="test-n1",machine_type="n1-slim",price_tier="ondemand",project="testing-1",provider="gcp",region="us-central1"} 1
	instance_cpu_hourly_cost{family="n2",instance="test-n2",machine_type="n2-slim",price_tier="ondemand",project="testing",provider="gcp",region="us-central1"} 1
	instance_cpu_hourly_cost{family="n2",instance="test-n2",machine_type="n2-slim",price_tier="ondemand",project="testing-1",provider="gcp",region="us-central1"} 1
	# HELP instance_memory_hourly_cost The hourly cost of a GKE instance
	# TYPE instance_memory_hourly_cost gauge
	instance_memory_hourly_cost{family="n1",instance="test-n1-spot",machine_type="n1-slim",price_tier="spot",project="testing",provider="gcp",region="us-central1"} 1
	instance_memory_hourly_cost{family="n1",instance="test-n1-spot",machine_type="n1-slim",price_tier="spot",project="testing-1",provider="gcp",region="us-central1"} 1
	instance_memory_hourly_cost{family="n1",instance="test-n1",machine_type="n1-slim",price_tier="ondemand",project="testing",provider="gcp",region="us-central1"} 1
	instance_memory_hourly_cost{family="n1",instance="test-n1",machine_type="n1-slim",price_tier="ondemand",project="testing-1",provider="gcp",region="us-central1"} 1
	instance_memory_hourly_cost{family="n2",instance="test-n2",machine_type="n2-slim",price_tier="ondemand",project="testing",provider="gcp",region="us-central1"} 1
	instance_memory_hourly_cost{family="n2",instance="test-n2",machine_type="n2-slim",price_tier="ondemand",project="testing-1",provider="gcp",region="us-central1"} 1
`), metricsNames...)
			assert.NoError(t, err)
		})
	}
}

func TestCollector_GetPricing(t *testing.T) {
	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := &compute.InstanceAggregatedList{
			Items: map[string]compute.InstancesScopedList{
				"projects/testing/zones/us-central1-a": {
					Instances: []*compute.Instance{
						{
							Name:        "test-n1",
							MachineType: "abc/n1-slim",
							Zone:        "testing/us-central1-a",
							Scheduling: &compute.Scheduling{
								ProvisioningModel: "test",
							},
						},
						{
							Name:        "test-n2",
							MachineType: "abc/n2-slim",
							Zone:        "testing/us-central1-a",
							Scheduling: &compute.Scheduling{
								ProvisioningModel: "test",
							},
						},
						{
							Name:        "test-n1-spot",
							MachineType: "abc/n1-slim",
							Zone:        "testing/us-central1-a",
							Scheduling: &compute.Scheduling{
								ProvisioningModel: "SPOT",
							},
						},
					},
				},
			},
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

		billingpb.RegisterCloudCatalogServer(gsrv, &fakeCloudCatalogServer{})
		cloudCatalagClient, err := billingv1.NewCloudCatalogClient(context.Background(),
			option.WithEndpoint(l.Addr().String()),
			option.WithoutAuthentication(),
			option.WithGRPCDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())),
		)

		collector.billingService = cloudCatalagClient

		require.NotNil(t, collector)

		err = collector.Collect()
		require.NoError(t, err)

		pricingMap = collector.PricingMap
		err = collector.Collect()
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
		billingpb.RegisterCloudCatalogServer(gsrv, &fakeCloudCatalogServerSlimResults{})
		cloudCatalogClient, _ := billingv1.NewCloudCatalogClient(context.Background(),
			option.WithEndpoint(l.Addr().String()),
			option.WithoutAuthentication(),
			option.WithGRPCDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())),
		)

		collector.billingService = cloudCatalogClient
		collector.NextScrape = time.Now().Add(-1 * time.Minute)
		err = collector.Collect()
		require.NotEqual(t, pricingMap, collector.PricingMap)
	})
}
