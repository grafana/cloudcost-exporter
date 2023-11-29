package gcs

import (
	"testing"

	"cloud.google.com/go/billing/apiv1/billingpb"
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
