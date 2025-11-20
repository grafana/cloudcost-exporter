package gcs

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	billingv1 "cloud.google.com/go/billing/apiv1"
	"cloud.google.com/go/billing/apiv1/billingpb"
	computeapiv1 "cloud.google.com/go/compute/apiv1"
	"cloud.google.com/go/storage"
	"github.com/grafana/cloudcost-exporter/pkg/google/client"
	mock_client "github.com/grafana/cloudcost-exporter/pkg/google/client/mocks"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
	"google.golang.org/api/option"

	"google.golang.org/genproto/googleapis/type/money"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func TestNew(t *testing.T) {
	ctrl := gomock.NewController(t)
	gcpClient := client.NewMock("project-1",
		0,
		mock_client.NewMockRegionsClient(ctrl),
		mock_client.NewMockStorageClientInterface(ctrl),
		nil,
		nil,
		nil,
	)

	t.Run("should return a non-nil client", func(t *testing.T) {
		gcsCollector, err := New(&Config{
			ProjectId: "project-1",
		}, gcpClient)
		assert.NoError(t, err)
		assert.NotNil(t, gcsCollector)
	})

	t.Run("collectorName should be GCS", func(t *testing.T) {
		gcsCollector, _ := New(&Config{
			ProjectId: "project-1",
		}, gcpClient)
		assert.Equal(t, "GCS", gcsCollector.Name())
	})
}

type fakeCloudBillingServer struct {
	billingpb.UnimplementedCloudCatalogServer
}

func (s *fakeCloudBillingServer) ListServices(_ context.Context, _ *billingpb.ListServicesRequest) (*billingpb.ListServicesResponse, error) {
	return &billingpb.ListServicesResponse{
		Services: []*billingpb.Service{
			{
				Name:        "services/6F81-5844-456A",
				DisplayName: "Cloud Storage",
			},
		},
	}, nil
}

func (s *fakeCloudBillingServer) ListSkus(_ context.Context, _ *billingpb.ListSkusRequest) (*billingpb.ListSkusResponse, error) {
	return &billingpb.ListSkusResponse{
		Skus: []*billingpb.Sku{
			{
				Name:        "services/6F81-5844-456A/skus/0001-0001-0001",
				Description: "US Regional Standard Storage",
				Category: &billingpb.Category{
					ServiceDisplayName: "Cloud Storage",
					ResourceGroup:      "Storage",
					ResourceFamily:     "Storage",
				},
				ServiceRegions: []string{"us-east1"},
				PricingInfo: []*billingpb.PricingInfo{
					{PricingExpression: &billingpb.PricingExpression{
						UsageUnitDescription: gibDay,
						TieredRates: []*billingpb.PricingExpression_TierRate{
							{UnitPrice: &money.Money{Nanos: 0}},
							{StartUsageAmount: 5, UnitPrice: &money.Money{Nanos: 4000000}},
						},
					},
					},
				},
			},
			{
				Name:        "services/6F81-5844-456A/skus/0001-0001-0001",
				Description: "US Regional Standard Storage Durable Reduced Availability",
				Category: &billingpb.Category{
					ServiceDisplayName: "Cloud Storage",
					ResourceGroup:      "Storage",
					ResourceFamily:     "Storage",
				},
			},
			{
				Name:        "services/6F81-5844-456A/skus/0001-0001-0001",
				Description: "US Regional Multi-Region Storage",
				Category: &billingpb.Category{
					ServiceDisplayName: "Cloud Storage",
					ResourceGroup:      "Storage",
					ResourceFamily:     "Storage",
				},
				ServiceRegions: []string{"us-east1"},
				PricingInfo: []*billingpb.PricingInfo{
					{PricingExpression: &billingpb.PricingExpression{
						UsageUnitDescription: gibMonthly,
						TieredRates: []*billingpb.PricingExpression_TierRate{
							{UnitPrice: &money.Money{Nanos: 0}},
							{StartUsageAmount: 5, UnitPrice: &money.Money{Nanos: 4000000}},
						},
					},
					},
				},
			},
			{
				Name:        "services/6F81-5844-456A/skus/0001-0001-0001",
				Description: "Standard Storage US Regional Ops Class A",
				Category: &billingpb.Category{
					ServiceDisplayName: "Cloud Storage",
					ResourceGroup:      "Storage Ops",
					ResourceFamily:     "Storage",
				},
				ServiceRegions: []string{"us-east1"},
				PricingInfo: []*billingpb.PricingInfo{
					{PricingExpression: &billingpb.PricingExpression{
						UsageUnitDescription: gibMonthly,
						TieredRates: []*billingpb.PricingExpression_TierRate{
							{UnitPrice: &money.Money{Nanos: 0}},
							{StartUsageAmount: 5, UnitPrice: &money.Money{Nanos: 4000000}},
						},
					},
					},
				},
			},
			{
				Name:        "services/6F81-5844-456A/skus/0001-0001-0001",
				Description: "Standard Storage US Regional Early Delete",
				Category: &billingpb.Category{
					ServiceDisplayName: "Cloud Storage",
					ResourceGroup:      "Storage",
				},
			},
			{
				Name:        "services/6F81-5844-456A/skus/0001-0001-0002",
				Description: "US Multi-Region Data Retrieval",
			},
			{
				Name:        "services/6F81-5844-456A/skus/0001-0001-0003",
				Description: "Networking of some kind",
				Category: &billingpb.Category{
					ResourceGroup:  "Network",
					ResourceFamily: "Network",
				},
			},
			{
				Name:        "services/6F81-5844-456A/skus/0001-0001-0004",
				Description: "Autoclass Management Fee",
			},
			{
				Name:        "services/6F81-5844-456A/skus/0001-0001-0005",
				Description: "Bucket Tagging Storage",
			},
		},
	}, nil
}

func TestGetServiceNameByReadableName(t *testing.T) {
	// We can't follow AWS's example as the CloudCatalogClient returns an iterator that has private fields that we can't easily override
	// Let's try to see if we can use an httptest server to mock the response
	tests := map[string]struct {
		service string
		want    string
		wantErr assert.ErrorAssertionFunc
	}{
		"should return an error if the service is not found": {
			service: "Does not exist",
			want:    "",
			wantErr: assert.Error,
		},
		"should return the service name": {
			service: "Cloud Storage",
			want:    "services/6F81-5844-456A",
			wantErr: assert.NoError,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			l, err := net.Listen("tcp", "localhost:0")
			assert.NoError(t, err)
			gsrv := grpc.NewServer()
			defer gsrv.Stop()
			go func() {
				if err = gsrv.Serve(l); err != nil {
					t.Errorf("failed to serve: %v", err)
				}
			}()

			billingpb.RegisterCloudCatalogServer(gsrv, &fakeCloudBillingServer{})
			catalogClient, err := billingv1.NewCloudCatalogClient(t.Context(),
				option.WithEndpoint(l.Addr().String()),
				option.WithoutAuthentication(),
				option.WithGRPCDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())))

			gcpClient := client.NewMock("", 0, nil, nil, catalogClient, nil, nil)

			assert.NoError(t, err)
			ctx := t.Context()
			got, err := gcpClient.GetServiceName(ctx, tt.service)
			if !tt.wantErr(t, err, fmt.Sprintf("GetServiceNameByReadableName(%v, %v, %v)", ctx, catalogClient, tt.service)) {
				return
			}
			assert.Equalf(t, tt.want, got, "GetServiceNameByReadableName(%v, %v, %v)", ctx, catalogClient, tt.want)
		})

	}
}

func TestCollector_Collect(t *testing.T) {
	regionsHttptestServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"items": [{"name": "us-east1", "description": "us-east1", "resourceGroup": "Storage"}]}`))
	}))
	regionsClient, err := computeapiv1.NewRegionsRESTClient(t.Context(), option.WithoutAuthentication(), option.WithEndpoint(regionsHttptestServer.URL))
	assert.NoError(t, err)
	assert.NotNil(t, regionsClient)

	storageHttptestServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"items": [
	{"name": "testbucket-1", "location": "US-EAST1", "storageClass": "STANDARD", "locationType": "region"},
	{"name": "testbucket-2", "location": "US", "storageClass": "STANDARD", "locationType": "multi-region"}
]}`))
	}))

	storageClient, err := storage.NewClient(t.Context(), option.WithoutAuthentication(), option.WithEndpoint(storageHttptestServer.URL))
	assert.NoError(t, err)
	assert.NotNil(t, storageClient)

	l, err := net.Listen("tcp", "localhost:0")
	assert.NoError(t, err)

	gsrv := grpc.NewServer()
	defer gsrv.Stop()
	go func() {
		if err = gsrv.Serve(l); err != nil {
			t.Errorf("failed to serve: %v", err)
		}
	}()
	billingpb.RegisterCloudCatalogServer(gsrv, &fakeCloudBillingServer{})
	cloudCatalogClient, err := billingv1.NewCloudCatalogClient(t.Context(),
		option.WithEndpoint(l.Addr().String()),
		option.WithoutAuthentication(),
		option.WithGRPCDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())))

	gcpClient := client.NewMock("project-1", 0, regionsClient, storageClient, cloudCatalogClient, nil, nil)

	assert.NoError(t, err)
	collector, err := New(&Config{
		ProjectId: "project-1",
	}, gcpClient)

	assert.NoError(t, err)
	assert.NotNil(t, collector)

	ch := make(chan prometheus.Metric)
	up := collector.CollectMetrics(ch)
	assert.Equal(t, 1.0, up)

	r := prometheus.NewPedanticRegistry()
	err = collector.Register(r)
	assert.NoError(t, err)

	metricNames := []string{
		"cloudcost_gcp_gcs_storage_by_location_usd_per_gibyte_hour",
		"cloudcost_gcp_gcs_storage_discount_by_location_usd_per_gibyte_hour",
		"cloudcost_gcp_gcs_operation_by_location_usd_per_krequest",
		"cloudcost_gcp_gcs_operation_discount_by_location_usd_per_krequest",
		"cloudcost_gcp_gcs_bucket_info",
	}
	err = testutil.CollectAndCompare(r, strings.NewReader(`
   # HELP cloudcost_gcp_gcs_bucket_info Location, location_type and storage class information for a GCS object by bucket_name
# TYPE cloudcost_gcp_gcs_bucket_info gauge
cloudcost_gcp_gcs_bucket_info{bucket_name="testbucket-1",location="us-east1",location_type="region",storage_class="STANDARD"} 1
cloudcost_gcp_gcs_bucket_info{bucket_name="testbucket-2",location="us",location_type="multi-region",storage_class="STANDARD"} 1
# HELP cloudcost_gcp_gcs_operation_by_location_usd_per_krequest Operation cost of GCS objects by location, storage_class, and opclass. Cost represented in USD/(1k req)
# TYPE cloudcost_gcp_gcs_operation_by_location_usd_per_krequest gauge
cloudcost_gcp_gcs_operation_by_location_usd_per_krequest{location="us-east1",opclass="class-a",storage_class="REGIONAL"} 0.004
# HELP cloudcost_gcp_gcs_operation_discount_by_location_usd_per_krequest Discount for operation cost of GCS objects by location, storage_class, and opclass. Cost represented in USD/(1k req)
# TYPE cloudcost_gcp_gcs_operation_discount_by_location_usd_per_krequest gauge
cloudcost_gcp_gcs_operation_discount_by_location_usd_per_krequest{location_type="dual-region",opclass="class-a",storage_class="MULTI_REGIONAL"} 0.595
cloudcost_gcp_gcs_operation_discount_by_location_usd_per_krequest{location_type="dual-region",opclass="class-a",storage_class="STANDARD"} 0.595
cloudcost_gcp_gcs_operation_discount_by_location_usd_per_krequest{location_type="dual-region",opclass="class-b",storage_class="MULTI_REGIONAL"} 0.19
cloudcost_gcp_gcs_operation_discount_by_location_usd_per_krequest{location_type="dual-region",opclass="class-b",storage_class="STANDARD"} 0.19
cloudcost_gcp_gcs_operation_discount_by_location_usd_per_krequest{location_type="multi-region",opclass="class-a",storage_class="COLDLINE"} 0.795
cloudcost_gcp_gcs_operation_discount_by_location_usd_per_krequest{location_type="multi-region",opclass="class-a",storage_class="MULTI_REGIONAL"} 0.595
cloudcost_gcp_gcs_operation_discount_by_location_usd_per_krequest{location_type="multi-region",opclass="class-a",storage_class="NEARLINE"} 0.595
cloudcost_gcp_gcs_operation_discount_by_location_usd_per_krequest{location_type="multi-region",opclass="class-a",storage_class="STANDARD"} 0.595
cloudcost_gcp_gcs_operation_discount_by_location_usd_per_krequest{location_type="multi-region",opclass="class-b",storage_class="COLDLINE"} 0.19
cloudcost_gcp_gcs_operation_discount_by_location_usd_per_krequest{location_type="multi-region",opclass="class-b",storage_class="MULTI_REGIONAL"} 0.19
cloudcost_gcp_gcs_operation_discount_by_location_usd_per_krequest{location_type="multi-region",opclass="class-b",storage_class="NEARLINE"} 0.19
cloudcost_gcp_gcs_operation_discount_by_location_usd_per_krequest{location_type="multi-region",opclass="class-b",storage_class="STANDARD"} 0.19
cloudcost_gcp_gcs_operation_discount_by_location_usd_per_krequest{location_type="region",opclass="class-a",storage_class="ARCHIVE"} 0.19
cloudcost_gcp_gcs_operation_discount_by_location_usd_per_krequest{location_type="region",opclass="class-a",storage_class="COLDLINE"} 0.595
cloudcost_gcp_gcs_operation_discount_by_location_usd_per_krequest{location_type="region",opclass="class-a",storage_class="NEARLINE"} 0.19
cloudcost_gcp_gcs_operation_discount_by_location_usd_per_krequest{location_type="region",opclass="class-a",storage_class="REGIONAL"} 0.19
cloudcost_gcp_gcs_operation_discount_by_location_usd_per_krequest{location_type="region",opclass="class-a",storage_class="STANDARD"} 0.19
cloudcost_gcp_gcs_operation_discount_by_location_usd_per_krequest{location_type="region",opclass="class-b",storage_class="ARCHIVE"} 0.19
cloudcost_gcp_gcs_operation_discount_by_location_usd_per_krequest{location_type="region",opclass="class-b",storage_class="COLDLINE"} 0.19
cloudcost_gcp_gcs_operation_discount_by_location_usd_per_krequest{location_type="region",opclass="class-b",storage_class="NEARLINE"} 0.19
cloudcost_gcp_gcs_operation_discount_by_location_usd_per_krequest{location_type="region",opclass="class-b",storage_class="REGIONAL"} 0.19
cloudcost_gcp_gcs_operation_discount_by_location_usd_per_krequest{location_type="region",opclass="class-b",storage_class="STANDARD"} 0.19
# HELP cloudcost_gcp_gcs_storage_discount_by_location_usd_per_gibyte_hour Discount for storage cost of GCS objects by location and storage_class. Cost represented in USD/(GiB*h)
# TYPE cloudcost_gcp_gcs_storage_discount_by_location_usd_per_gibyte_hour gauge
cloudcost_gcp_gcs_storage_discount_by_location_usd_per_gibyte_hour{location="asia",storage_class="ARCHIVE"} 0
cloudcost_gcp_gcs_storage_discount_by_location_usd_per_gibyte_hour{location="asia",storage_class="COLDLINE"} 0
cloudcost_gcp_gcs_storage_discount_by_location_usd_per_gibyte_hour{location="asia",storage_class="MULTI_REGIONAL"} 0
cloudcost_gcp_gcs_storage_discount_by_location_usd_per_gibyte_hour{location="asia",storage_class="NEARLINE"} 0
cloudcost_gcp_gcs_storage_discount_by_location_usd_per_gibyte_hour{location="asia",storage_class="STANDARD"} 0
cloudcost_gcp_gcs_storage_discount_by_location_usd_per_gibyte_hour{location="asia1",storage_class="ARCHIVE"} 0
cloudcost_gcp_gcs_storage_discount_by_location_usd_per_gibyte_hour{location="asia1",storage_class="COLDLINE"} 0
cloudcost_gcp_gcs_storage_discount_by_location_usd_per_gibyte_hour{location="asia1",storage_class="MULTI_REGIONAL"} 0
cloudcost_gcp_gcs_storage_discount_by_location_usd_per_gibyte_hour{location="asia1",storage_class="NEARLINE"} 0
cloudcost_gcp_gcs_storage_discount_by_location_usd_per_gibyte_hour{location="asia1",storage_class="STANDARD"} 0
cloudcost_gcp_gcs_storage_discount_by_location_usd_per_gibyte_hour{location="eu",storage_class="ARCHIVE"} 0
cloudcost_gcp_gcs_storage_discount_by_location_usd_per_gibyte_hour{location="eu",storage_class="COLDLINE"} 0
cloudcost_gcp_gcs_storage_discount_by_location_usd_per_gibyte_hour{location="eu",storage_class="MULTI_REGIONAL"} 0
cloudcost_gcp_gcs_storage_discount_by_location_usd_per_gibyte_hour{location="eu",storage_class="NEARLINE"} 0
cloudcost_gcp_gcs_storage_discount_by_location_usd_per_gibyte_hour{location="eu",storage_class="STANDARD"} 0
cloudcost_gcp_gcs_storage_discount_by_location_usd_per_gibyte_hour{location="eur4",storage_class="ARCHIVE"} 0
cloudcost_gcp_gcs_storage_discount_by_location_usd_per_gibyte_hour{location="eur4",storage_class="COLDLINE"} 0
cloudcost_gcp_gcs_storage_discount_by_location_usd_per_gibyte_hour{location="eur4",storage_class="MULTI_REGIONAL"} 0
cloudcost_gcp_gcs_storage_discount_by_location_usd_per_gibyte_hour{location="eur4",storage_class="NEARLINE"} 0
cloudcost_gcp_gcs_storage_discount_by_location_usd_per_gibyte_hour{location="eur4",storage_class="STANDARD"} 0
cloudcost_gcp_gcs_storage_discount_by_location_usd_per_gibyte_hour{location="nam4",storage_class="ARCHIVE"} 0
cloudcost_gcp_gcs_storage_discount_by_location_usd_per_gibyte_hour{location="nam4",storage_class="COLDLINE"} 0
cloudcost_gcp_gcs_storage_discount_by_location_usd_per_gibyte_hour{location="nam4",storage_class="MULTI_REGIONAL"} 0
cloudcost_gcp_gcs_storage_discount_by_location_usd_per_gibyte_hour{location="nam4",storage_class="NEARLINE"} 0
cloudcost_gcp_gcs_storage_discount_by_location_usd_per_gibyte_hour{location="nam4",storage_class="STANDARD"} 0
cloudcost_gcp_gcs_storage_discount_by_location_usd_per_gibyte_hour{location="us",storage_class="ARCHIVE"} 0
cloudcost_gcp_gcs_storage_discount_by_location_usd_per_gibyte_hour{location="us",storage_class="COLDLINE"} 0
cloudcost_gcp_gcs_storage_discount_by_location_usd_per_gibyte_hour{location="us",storage_class="MULTI_REGIONAL"} 0
cloudcost_gcp_gcs_storage_discount_by_location_usd_per_gibyte_hour{location="us",storage_class="NEARLINE"} 0
cloudcost_gcp_gcs_storage_discount_by_location_usd_per_gibyte_hour{location="us",storage_class="STANDARD"} 0
cloudcost_gcp_gcs_storage_discount_by_location_usd_per_gibyte_hour{location="us-east1",storage_class="ARCHIVE"} 0
cloudcost_gcp_gcs_storage_discount_by_location_usd_per_gibyte_hour{location="us-east1",storage_class="COLDLINE"} 0
cloudcost_gcp_gcs_storage_discount_by_location_usd_per_gibyte_hour{location="us-east1",storage_class="NEARLINE"} 0
cloudcost_gcp_gcs_storage_discount_by_location_usd_per_gibyte_hour{location="us-east1",storage_class="REGIONAL"} 0
cloudcost_gcp_gcs_storage_discount_by_location_usd_per_gibyte_hour{location="us-east1",storage_class="STANDARD"} 0
# HELP cloudcost_gcp_gcs_storage_by_location_usd_per_gibyte_hour Storage cost of GCS objects by location and storage_class. Cost represented in USD/(GiB*h)
# TYPE cloudcost_gcp_gcs_storage_by_location_usd_per_gibyte_hour gauge
cloudcost_gcp_gcs_storage_by_location_usd_per_gibyte_hour{location="us-east1",storage_class="MULTI_REGIONAL"} 5.376344086021506e-06
cloudcost_gcp_gcs_storage_by_location_usd_per_gibyte_hour{location="us-east1",storage_class="REGIONAL"} 0.00016666666666666666
`), metricNames...)
	assert.NoError(t, err)
}
