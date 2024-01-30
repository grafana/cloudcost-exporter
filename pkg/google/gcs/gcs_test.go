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
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"google.golang.org/api/option"

	"google.golang.org/genproto/googleapis/type/money"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/grafana/cloudcost-exporter/mocks/pkg/google/gcs"
	"github.com/grafana/cloudcost-exporter/pkg/google/billing"
)

func TestStorageclassFromSkuDescription(t *testing.T) {
	tt := map[string]struct {
		exp string
	}{
		"Dual-Region Standard Class B Operation": {
			"MULTI_REGIONAL",
		},
		"Multi-Region Nearline Class A Operations": {
			"NEARLINE",
		},
		"Coldline Storage US Multi-region": {
			"COLDLINE",
		},
		"Durable Reduced Availability Multi-region": {
			"DRA",
		},
		"Standard Storage US Regional": {
			"REGIONAL",
		},
		"Standard Storage London": {
			"REGIONAL",
		},
		"Archive Storage Belgium Dual-region": {
			"ARCHIVE",
		},
	}

	for name, f := range tt {
		t.Run(name, func(t *testing.T) {
			got := StorageClassFromSkuDescription(name, "any_regular_region")
			if got != f.exp {
				t.Errorf("expecting storageclass %s, got %s", f.exp, got)
			}
		})
	}
}

type StorageRegion struct {
	sku    string
	region string
}

func TestStorageclassFromSkuDescriptionExceptions(t *testing.T) {
	tt := map[StorageRegion]struct {
		exp string
	}{
		{
			sku:    "Standard Storage South Carolina Dual-region",
			region: "us-east1",
		}: {
			exp: "REGIONAL",
		},
		{
			sku:    "Standard Storage Iowa Dual-region",
			region: "us-central1",
		}: {
			exp: "REGIONAL",
		},
		{
			sku:    "Standard Storage South Carolina Dual-region",
			region: "nam4",
		}: {
			exp: "MULTI_REGIONAL",
		},
	}

	for storageRegion, f := range tt {
		t.Run(storageRegion.sku+"-"+storageRegion.region, func(t *testing.T) {
			got := StorageClassFromSkuDescription(storageRegion.sku, storageRegion.region)
			if got != f.exp {
				t.Errorf("expecting storageclass %s, got %s", f.exp, got)
			}
		})
	}
}

func TestPriceFromSku(t *testing.T) {
	sku := billingpb.Sku{
		PricingInfo: []*billingpb.PricingInfo{
			{PricingExpression: &billingpb.PricingExpression{
				TieredRates: []*billingpb.PricingExpression_TierRate{
					{UnitPrice: &money.Money{Nanos: 0}},
					{StartUsageAmount: 5, UnitPrice: &money.Money{Nanos: 4000000}},
				},
			}},
		},
	}
	got, err := getPriceFromSku(&sku)
	exp := 0.004
	if err != nil {
		t.Errorf("failed to parse sku")
	}
	if got != exp {
		t.Errorf("expect %f but got %f", exp, got)
	}
}

func TestMisformedPricingInfoFromSku(t *testing.T) {
	tt := []struct {
		sku   *billingpb.Sku
		descr string
	}{
		{
			sku: &billingpb.Sku{
				PricingInfo: []*billingpb.PricingInfo{},
			},
			descr: "should fail to parse sku with empty PricingInfo",
		},
		{
			sku: &billingpb.Sku{
				PricingInfo: []*billingpb.PricingInfo{
					{PricingExpression: &billingpb.PricingExpression{
						TieredRates: []*billingpb.PricingExpression_TierRate{},
					}},
				},
			},
			descr: "shoud fail to parse sku with empty TieredRates",
		},
	}

	for _, testcase := range tt {
		_, err := getPriceFromSku(testcase.sku)
		if err == nil {
			t.Errorf(testcase.descr)
		}
	}
}

func TestNew(t *testing.T) {
	regionsClient := gcs.NewRegionsClient(t)
	storageClient := gcs.NewStorageClientInterface(t)

	t.Run("should return a non-nil client", func(t *testing.T) {
		gcsCollector, err := New(&Config{
			ProjectId: "project-1",
		}, nil, regionsClient, storageClient)
		assert.NoError(t, err)
		assert.NotNil(t, gcsCollector)
	})

	t.Run("collectorName should be GCS", func(t *testing.T) {
		gcsCollector, _ := New(&Config{
			ProjectId: "project-1",
		}, nil, regionsClient, storageClient)
		assert.Equal(t, "GCS", gcsCollector.Name())
	})
}

func TestOpClassFromSkuDescription(t *testing.T) {
	tests := map[string]struct {
		str  string
		want string
	}{
		"OpsClass without class-a or class-b": {
			str:  "Standard Storage US Regional",
			want: "Standard Storage US Regional",
		},
		"OpsClass with class-a": {
			str:  "Standard Storage US Regional Class A Operations",
			want: "class-a",
		},
		"OpsClass with class-b": {
			str:  "Standard Storage US Regional Class B Operations",
			want: "class-b",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equalf(t, tt.want, OpClassFromSkuDescription(tt.str), "OpClassFromSkuDescription(%v)", tt.want)
		})
	}
}

func TestRegionNameSameAsStackdriver(t *testing.T) {
	tests := map[string]struct {
		region string
		want   string
	}{
		"region collectorName is same as stackdriver": {
			region: "us-east1",
			want:   "us-east1",
		},
		"region collectorName is not same as stackdriver": {
			region: "europe",
			want:   "eu",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equalf(t, tt.want, RegionNameSameAsStackdriver(tt.region), "RegionNameSameAsStackdriver(%v)", tt.region)
		})
	}
}

func Test_parseOpSku(t *testing.T) {
	tests := map[string]struct {
		sku *billingpb.Sku
		err error
	}{
		"should fail to parse sku with no pricing info": {
			sku: &billingpb.Sku{
				Category: &billingpb.Category{
					ServiceDisplayName: "Compute Engine",
				},
				ServiceRegions: []string{"us-east1"},
			},
			err: invalidSku,
		},
		"should fail to parse sku with tagging": {
			sku: &billingpb.Sku{
				Category: &billingpb.Category{
					ServiceDisplayName: "Tagging Test",
				},
				Description: "Tagging",
			},
			err: taggingError,
		},
		"should parse a sku with pricing and description": {
			sku: &billingpb.Sku{
				Category: &billingpb.Category{
					ServiceDisplayName: "Compute Engine",
				},
				ServiceRegions: []string{"us-east1"},
				PricingInfo: []*billingpb.PricingInfo{
					{PricingExpression: &billingpb.PricingExpression{
						TieredRates: []*billingpb.PricingExpression_TierRate{
							{UnitPrice: &money.Money{Nanos: 0}},
							{StartUsageAmount: 5, UnitPrice: &money.Money{Nanos: 4000000}},
						},
					},
					},
				},
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := parseOpSku(tt.sku, NewMetrics())
			assert.ErrorIs(t, err, tt.err)
		})
	}
}
func Test_parseStorageSku(t *testing.T) {
	tests := map[string]struct {
		sku *billingpb.Sku
		err error
	}{
		"should fail to parse sku with no pricing info": {
			sku: &billingpb.Sku{
				Category: &billingpb.Category{
					ServiceDisplayName: "Compute Engine",
				},
				ServiceRegions: []string{"us-east1"},
			},
			err: invalidSku,
		},
		"should fail to parse sku with unknown pricing unit": {
			sku: &billingpb.Sku{
				Category: &billingpb.Category{
					ServiceDisplayName: "Compute Engine",
				},
				ServiceRegions: []string{"us-east1"},
				PricingInfo: []*billingpb.PricingInfo{
					{PricingExpression: &billingpb.PricingExpression{
						UsageUnitDescription: "unknown",
						TieredRates: []*billingpb.PricingExpression_TierRate{
							{UnitPrice: &money.Money{Nanos: 0}},
							{StartUsageAmount: 5, UnitPrice: &money.Money{Nanos: 4000000}},
						},
					},
					},
				},
			},
			err: unknownPricingUnit,
		},
		"should parse a sku with one pricing unit with gibDaily": {
			sku: &billingpb.Sku{
				Category: &billingpb.Category{
					ServiceDisplayName: "Compute Engine",
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
			err: nil,
		},
		"should parse a sku with one pricing unit with gibMonthly": {
			sku: &billingpb.Sku{
				Category: &billingpb.Category{
					ServiceDisplayName: "Compute Engine",
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
			err: nil,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := parseStorageSku(tt.sku, NewMetrics())
			assert.ErrorIs(t, err, tt.err)
		})
	}
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
			client, err := billingv1.NewCloudCatalogClient(context.Background(),
				option.WithEndpoint(l.Addr().String()),
				option.WithoutAuthentication(),
				option.WithGRPCDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())))

			assert.NoError(t, err)
			ctx := context.Background()
			got, err := billing.GetServiceName(ctx, client, tt.service)
			if !tt.wantErr(t, err, fmt.Sprintf("GetServiceNameByReadableName(%v, %v, %v)", ctx, client, tt.service)) {
				return
			}
			assert.Equalf(t, tt.want, got, "GetServiceNameByReadableName(%v, %v, %v)", ctx, client, tt.want)
		})

	}
}

func TestCollector_Collect(t *testing.T) {
	regionsHttptestServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"items": [{"name": "us-east1", "description": "us-east1", "resourceGroup": "Storage"}]}`))
	}))
	regionsClient, err := computeapiv1.NewRegionsRESTClient(context.Background(), option.WithoutAuthentication(), option.WithEndpoint(regionsHttptestServer.URL))
	assert.NoError(t, err)
	assert.NotNil(t, regionsClient)

	storageHttptestServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"items": [
	{"name": "testbucket-1", "location": "US-EAST1", "storageClass": "STANDARD", "locationType": "region"},
	{"name": "testbucket-2", "location": "US", "storageClass": "STANDARD", "locationType": "multi-region"}
]}`))
	}))

	storageClient, err := storage.NewClient(context.Background(), option.WithoutAuthentication(), option.WithEndpoint(storageHttptestServer.URL))
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
	cloudCatalogClient, err := billingv1.NewCloudCatalogClient(context.Background(),
		option.WithEndpoint(l.Addr().String()),
		option.WithoutAuthentication(),
		option.WithGRPCDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())))

	assert.NoError(t, err)
	collector, err := New(&Config{
		ProjectId: "project-1",
	}, cloudCatalogClient, regionsClient, storageClient)

	assert.NoError(t, err)
	assert.NotNil(t, collector)

	up := collector.CollectMetrics(nil)
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
