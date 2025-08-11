package client

import (
	"testing"

	"cloud.google.com/go/billing/apiv1/billingpb"
	"github.com/grafana/cloudcost-exporter/pkg/google/metrics"
	"github.com/stretchr/testify/assert"
	"google.golang.org/genproto/googleapis/type/money"
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
			got := storageClassFromSkuDescription(name, "any_regular_region")
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
			t.Error(testcase.descr)
		}
	}
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
			assert.Equalf(t, tt.want, opClassFromSkuDescription(tt.str), "OpClassFromSkuDescription(%v)", tt.want)
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
			err: errInvalidSKU,
		},
		"should fail to parse sku with tagging": {
			sku: &billingpb.Sku{
				Category: &billingpb.Category{
					ServiceDisplayName: "Tagging Test",
				},
				Description: "Tagging",
			},
			err: errTaggingNotSupported,
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
			err := parseOpSku(tt.sku, metrics.NewMetrics())
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
			err: errInvalidSKU,
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
			err: errUnknownPricingUnit,
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
			err := parseStorageSku(tt.sku, metrics.NewMetrics())
			assert.ErrorIs(t, err, tt.err)
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
			assert.Equalf(t, tt.want, regionNameSameAsStackdriver(tt.region), "RegionNameSameAsStackdriver(%v)", tt.region)
		})
	}
}
