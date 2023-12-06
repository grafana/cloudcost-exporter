package gcs

import (
	"context"
	"fmt"
	"net"
	"testing"

	billing "cloud.google.com/go/billing/apiv1"
	"cloud.google.com/go/billing/apiv1/billingpb"
	"github.com/stretchr/testify/assert"
	"google.golang.org/api/option"
	"google.golang.org/genproto/googleapis/type/money"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/grafana/cloudcost-exporter/mocks/pkg/google/gcs"
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
			got := StorageClassFromSkuDescription(t.Name(), "any_regular_region")
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
	billingClient := gcs.NewCloudCatalogClient(t)
	regionsClient := gcs.NewRegionsClient(t)
	storageClient := gcs.NewStorageClientInterface(t)

	t.Run("should return a non-nil client", func(t *testing.T) {
		gcsCollector, err := New(&Config{
			ProjectId: "project-1",
		}, billingClient, regionsClient, storageClient)
		assert.NoError(t, err)
		assert.NotNil(t, gcsCollector)
	})

	t.Run("collectorName should be GCS", func(t *testing.T) {
		gcsCollector, _ := New(&Config{
			ProjectId: "project-1",
		}, billingClient, regionsClient, storageClient)
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
			err := parseOpSku(tt.sku)
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
			err := parseStorageSku(tt.sku)
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
			client, err := billing.NewCloudCatalogClient(context.Background(),
				option.WithEndpoint(l.Addr().String()),
				option.WithoutAuthentication(),
				option.WithGRPCDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())))

			assert.NoError(t, err)
			ctx := context.Background()
			got, err := GetServiceNameByReadableName(ctx, client, tt.service)
			if !tt.wantErr(t, err, fmt.Sprintf("GetServiceNameByReadableName(%v, %v, %v)", ctx, client, tt.service)) {
				return
			}
			assert.Equalf(t, tt.want, got, "GetServiceNameByReadableName(%v, %v, %v)", ctx, client, tt.want)
		})
	}
}
