package cloudsql

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	"cloud.google.com/go/billing/apiv1/billingpb"
	"github.com/grafana/cloudcost-exporter/pkg/google/client"
	"github.com/grafana/cloudcost-exporter/pkg/utils"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	computev1 "google.golang.org/api/compute/v1"
	"google.golang.org/api/option"
	sqladmin "google.golang.org/api/sqladmin/v1beta4"
	"google.golang.org/genproto/googleapis/type/money"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func newTestGCPClient(t *testing.T, computeHandlers map[string]any, sqlAdminHandlers map[string]any, skus []*billingpb.Sku) *client.Mock {
	t.Helper()

	computeSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if resp, ok := computeHandlers[r.URL.Path]; ok {
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		_ = json.NewEncoder(w).Encode(struct{}{})
	}))
	t.Cleanup(computeSrv.Close)

	sqlAdminSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if resp, ok := sqlAdminHandlers[r.URL.Path]; ok {
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		_ = json.NewEncoder(w).Encode(struct{}{})
	}))
	t.Cleanup(sqlAdminSrv.Close)

	// Set up gRPC server for billing API
	fakeBillingServer := &client.FakeCloudCatalogServerWithSKUs{
		ServiceName: "Cloud SQL",
		ServiceID:   "services/cloud-sql",
		Skus:        skus,
	}
	catalogClient := client.NewTestBillingClient(t, fakeBillingServer)

	computeService, err := computev1.NewService(context.Background(), option.WithoutAuthentication(), option.WithEndpoint(computeSrv.URL))
	require.NoError(t, err)

	sqlAdminService, err := sqladmin.NewService(context.Background(), option.WithoutAuthentication(), option.WithEndpoint(sqlAdminSrv.URL))
	require.NoError(t, err)

	return client.NewMock("test-project", 0, nil, nil, catalogClient, computeService, sqlAdminService)
}

type failingCatalogServer struct {
	billingpb.UnimplementedCloudCatalogServer
}

func (s *failingCatalogServer) ListServices(_ context.Context, _ *billingpb.ListServicesRequest) (*billingpb.ListServicesResponse, error) {
	return nil, status.Error(codes.Internal, "billing API unavailable")
}

type switchableCatalogServer struct {
	billingpb.UnimplementedCloudCatalogServer
	mu       sync.Mutex
	disabled bool
	skus     []*billingpb.Sku
}

func (s *switchableCatalogServer) disable() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.disabled = true
}

func (s *switchableCatalogServer) ListServices(_ context.Context, _ *billingpb.ListServicesRequest) (*billingpb.ListServicesResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.disabled {
		return nil, status.Error(codes.Unavailable, "billing server disabled")
	}
	return &billingpb.ListServicesResponse{
		Services: []*billingpb.Service{
			{Name: "services/cloud-sql", DisplayName: "Cloud SQL"},
		},
	}, nil
}

func (s *switchableCatalogServer) ListSkus(_ context.Context, _ *billingpb.ListSkusRequest) (*billingpb.ListSkusResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.disabled {
		return nil, status.Error(codes.Unavailable, "billing server disabled")
	}
	return &billingpb.ListSkusResponse{Skus: s.skus}, nil
}

func TestNew_FailsIfInitialSKUFetchFails(t *testing.T) {
	catalogClient := client.NewTestBillingClient(t, &failingCatalogServer{})

	computeSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(struct{}{})
	}))
	t.Cleanup(computeSrv.Close)

	sqlAdminSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(struct{}{})
	}))
	t.Cleanup(sqlAdminSrv.Close)

	computeService, err := computev1.NewService(context.Background(), option.WithoutAuthentication(), option.WithEndpoint(computeSrv.URL))
	require.NoError(t, err)

	sqlAdminService, err := sqladmin.NewService(context.Background(), option.WithoutAuthentication(), option.WithEndpoint(sqlAdminSrv.URL))
	require.NoError(t, err)

	gcpClient := client.NewMock("test-project", 0, nil, nil, catalogClient, computeService, sqlAdminService)
	config := &Config{Projects: "test-project", Logger: slog.New(slog.NewTextHandler(os.Stdout, nil))}

	_, err = New(context.Background(), config, gcpClient)
	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to initialise Cloud SQL pricing")
}

func TestCollect_UsesCachedSKUs(t *testing.T) {
	skus := []*billingpb.Sku{
		{
			SkuId: "test-sku-id",
			Category: &billingpb.Category{
				ServiceDisplayName: "Cloud SQL",
			},
			Description: "Cloud SQL: MYSQL db-f1-micro ZONAL instance running in test-region",
			GeoTaxonomy: &billingpb.GeoTaxonomy{
				Regions: []string{"test-region"},
			},
			PricingInfo: []*billingpb.PricingInfo{
				{
					PricingExpression: &billingpb.PricingExpression{
						TieredRates: []*billingpb.PricingExpression_TierRate{
							{
								UnitPrice: &money.Money{
									Nanos: 25000000, // $0.025 per hour
								},
							},
						},
					},
				},
			},
		},
	}

	billingSrv := &switchableCatalogServer{skus: skus}
	catalogClient := client.NewTestBillingClient(t, billingSrv)

	computeSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/projects/test-project/regions" {
			_ = json.NewEncoder(w).Encode(&computev1.RegionList{
				Items: []*computev1.Region{{Name: "test-region"}},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(struct{}{})
	}))
	t.Cleanup(computeSrv.Close)

	sqlAdminSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sql/v1beta4/projects/test-project/instances" {
			_ = json.NewEncoder(w).Encode(&sqladmin.InstancesListResponse{
				Items: []*sqladmin.DatabaseInstance{
					{
						Name:            "test-name",
						Region:          "test-region",
						ConnectionName:  "test-project:test-region:test-name",
						Settings:        &sqladmin.Settings{Tier: "db-f1-micro", AvailabilityType: "ZONAL"},
						DatabaseVersion: "MYSQL_8_0",
					},
				},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(struct{}{})
	}))
	t.Cleanup(sqlAdminSrv.Close)

	computeService, err := computev1.NewService(context.Background(), option.WithoutAuthentication(), option.WithEndpoint(computeSrv.URL))
	require.NoError(t, err)

	sqlAdminService, err := sqladmin.NewService(context.Background(), option.WithoutAuthentication(), option.WithEndpoint(sqlAdminSrv.URL))
	require.NoError(t, err)

	gcpClient := client.NewMock("test-project", 0, nil, nil, catalogClient, computeService, sqlAdminService)
	config := &Config{Projects: "test-project", Logger: slog.New(slog.NewTextHandler(os.Stdout, nil))}

	collector, err := New(context.Background(), config, gcpClient)
	require.NoError(t, err)

	// Disable the billing backend — Collect() must use SKUs cached at init
	billingSrv.disable()

	ch := make(chan prometheus.Metric, 10)
	err = collector.Collect(context.Background(), ch)
	require.NoError(t, err)

	select {
	case metric := <-ch:
		result := utils.ReadMetrics(metric)
		assert.Equal(t, "test-project:test-region:test-name", result.Labels["instance"])
		assert.InDelta(t, 0.025, result.Value, 1e-9)
	default:
		t.Fatal("expected a metric to be emitted from cached SKUs")
	}
}

func TestCollector(t *testing.T) {
	tests := []struct {
		name             string
		wantErr          bool
		regionsHandlers  map[string]any
		sqlAdminHandlers map[string]any
		skus             []*billingpb.Sku
	}{
		{
			name: "finds price for instance",
			regionsHandlers: map[string]any{
				"/projects/test-project/regions": &computev1.RegionList{
					Items: []*computev1.Region{
						{
							Name: "test-region",
						},
					},
				},
			},

			sqlAdminHandlers: map[string]any{
				"/sql/v1beta4/projects/test-project/instances": &sqladmin.InstancesListResponse{
					Items: []*sqladmin.DatabaseInstance{
						{
							Name:            "test-name",
							Region:          "test-region",
							ConnectionName:  "test-project:test-region:test-name",
							Settings:        &sqladmin.Settings{Tier: "db-f1-micro", AvailabilityType: "ZONAL"},
							DatabaseVersion: "MYSQL_8_0",
						},
					},
				},
			},
			skus: []*billingpb.Sku{
				{
					SkuId: "test-sku-id",
					Category: &billingpb.Category{
						ServiceDisplayName: "Cloud SQL",
					},
					Description: "Cloud SQL: MYSQL db-f1-micro ZONAL instance running in test-region",
					GeoTaxonomy: &billingpb.GeoTaxonomy{
						Regions: []string{"test-region"},
					},
					PricingInfo: []*billingpb.PricingInfo{
						{
							PricingExpression: &billingpb.PricingExpression{
								TieredRates: []*billingpb.PricingExpression_TierRate{
									{
										UnitPrice: &money.Money{
											Units: 0,
											Nanos: 25000000, // $0.025 per hour
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "custom pricing",
			regionsHandlers: map[string]any{
				"/projects/test-project/regions": &computev1.RegionList{
					Items: []*computev1.Region{
						{
							Name: "test-region",
						},
					},
				},
			},
			sqlAdminHandlers: map[string]any{
				"/sql/v1beta4/projects/test-project/instances": &sqladmin.InstancesListResponse{
					Items: []*sqladmin.DatabaseInstance{
						{
							Name:            "test-name",
							Region:          "test-region",
							ConnectionName:  "test-project:test-region:test-name",
							Settings:        &sqladmin.Settings{Tier: "db-custom-1-1", AvailabilityType: "ZONAL"},
							DatabaseVersion: "MYSQL_8_0",
						},
					},
				},
			},
			skus: []*billingpb.Sku{
				{
					SkuId: "cpu-sku-id",
					Category: &billingpb.Category{
						ServiceDisplayName: "Cloud SQL",
					},
					Description: "Cloud SQL: MYSQL CPU component for custom instances in test-region",
					GeoTaxonomy: &billingpb.GeoTaxonomy{
						Regions: []string{"test-region"},
					},
					PricingInfo: []*billingpb.PricingInfo{
						{
							PricingExpression: &billingpb.PricingExpression{
								UsageUnit: "h",
								TieredRates: []*billingpb.PricingExpression_TierRate{
									{
										UnitPrice: &money.Money{
											Units: 0,
											Nanos: 50000000, // $0.05 per vCPU per hour
										},
									},
								},
							},
						},
					},
				},
				{
					SkuId: "ram-sku-id",
					Category: &billingpb.Category{
						ServiceDisplayName: "Cloud SQL",
					},
					Description: "Cloud SQL: MYSQL RAM component for custom instances in test-region",
					GeoTaxonomy: &billingpb.GeoTaxonomy{
						Regions: []string{"test-region"},
					},
					PricingInfo: []*billingpb.PricingInfo{
						{
							PricingExpression: &billingpb.PricingExpression{
								UsageUnit: "GiBy.h",
								TieredRates: []*billingpb.PricingExpression_TierRate{
									{
										UnitPrice: &money.Money{
											Units: 0,
											Nanos: 10000000, // $0.01 per GB per hour
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gcpClient := newTestGCPClient(t, tt.regionsHandlers, tt.sqlAdminHandlers, tt.skus)
			config := &Config{Projects: "test-project", Logger: slog.New(slog.NewTextHandler(os.Stdout, nil))}
			collector, err := New(context.Background(), config, gcpClient)
			require.NoError(t, err)

			ch := make(chan prometheus.Metric, 1)
			err = collector.Collect(context.Background(), ch)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			select {
			case metric := <-ch:
				metricResult := utils.ReadMetrics(metric)
				close(ch)
				assert.NoError(t, err)
				labels := metricResult.Labels
				assert.Equal(t, "test-project:test-region:test-name", labels["instance"])
			default:
				t.Fatal("expected a metric to be collected")
			}
		})
	}

}

func TestGetAllCloudSQL(t *testing.T) {
	tests := []struct {
		name             string
		wantErr          bool
		regionsHandlers  map[string]any
		sqlAdminHandlers map[string]any
		skus             []*billingpb.Sku
	}{
		{
			name: "finds price for instance",
			regionsHandlers: map[string]any{
				"/projects/test-project/regions": &computev1.RegionList{
					Items: []*computev1.Region{
						{
							Name: "test-region",
						},
					},
				},
			},
			sqlAdminHandlers: map[string]any{
				"/sql/v1beta4/projects/test-project/instances": &sqladmin.InstancesListResponse{
					Items: []*sqladmin.DatabaseInstance{
						{
							Name:            "test-name",
							Region:          "test-region",
							ConnectionName:  "test-project:test-region:test-name",
							Settings:        &sqladmin.Settings{Tier: "db-f1-micro", AvailabilityType: "ZONAL"},
							DatabaseVersion: "MYSQL_8_0",
						},
					},
				},
			},
		},
		{
			name: "duplicates instances",
			sqlAdminHandlers: map[string]any{
				"/sql/v1beta4/projects/test-project/instances": &sqladmin.InstancesListResponse{
					Items: []*sqladmin.DatabaseInstance{
						{
							Name:            "test-name",
							Region:          "test-region",
							ConnectionName:  "test-project:test-region:test-name",
							Settings:        &sqladmin.Settings{Tier: "db-f1-micro", AvailabilityType: "ZONAL"},
							DatabaseVersion: "MYSQL_8_0",
						},
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gcpClient := newTestGCPClient(t, tt.regionsHandlers, tt.sqlAdminHandlers, nil)
			config := &Config{Projects: "test-project", Logger: slog.New(slog.NewTextHandler(os.Stdout, nil))}
			collector, err := New(context.Background(), config, gcpClient)
			require.NoError(t, err)

			instances, err := collector.getAllCloudSQL(context.Background())
			require.NoError(t, err)
			assert.Equal(t, 1, len(instances))
			assert.Equal(t, "test-project:test-region:test-name", instances[0].ConnectionName)
		})
	}
}
