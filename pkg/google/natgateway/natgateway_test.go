package natgateway

import (
	"testing"

	"cloud.google.com/go/billing/apiv1/billingpb"
	"github.com/grafana/cloudcost-exporter/pkg/google/common"
	money "google.golang.org/genproto/googleapis/type/money"
)

func Test_isCloudNATSKU(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"cloud nat exact", "Cloud NAT hourly charge", true},
		{"nat gateway phrase", "NAT Gateway data processed", true},
		{"irrelevant sku", "Compute Engine VM core", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isCloudNATSKU(tc.in)
			if got != tc.want {
				t.Fatalf("isCloudNATSKU(%q)=%v want %v", tc.in, got, tc.want)
			}
		})
	}
}

func Test_isDataProcessing(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"data processed", "Cloud NAT data processed (GB)", true},
		{"data processing", "Cloud NAT data processing charge", true},
		{"egress data", "Cloud NAT egress data fee", true},
		{"not data processing", "Cloud NAT hourly", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isDataProcessing(tc.in)
			if got != tc.want {
				t.Fatalf("isDataProcessing(%q)=%v want %v", tc.in, got, tc.want)
			}
		})
	}
}

func Test_firstTierPriceUSD(t *testing.T) {
	mkSku := func(units int64, nanos int32, conv float64) *billingpb.Sku {
		return &billingpb.Sku{
			PricingInfo: []*billingpb.PricingInfo{
				{
					PricingExpression: &billingpb.PricingExpression{
						TieredRates: []*billingpb.PricingExpression_TierRate{
							{UnitPrice: &money.Money{Units: units, Nanos: nanos}},
						},
						BaseUnitConversionFactor: conv,
					},
				},
			},
		}
	}

	cases := []struct {
		name string
		sku  *billingpb.Sku
		want float64
	}{
		{"simple dollars", mkSku(1, 0, 1), 1.0},
		{"with nanos", mkSku(0, 500000000, 1), 0.5},
		{"with conv factor", mkSku(3, 0, 3), 1.0},
		{"nil sku", nil, 0},
		{"no pricing info", &billingpb.Sku{}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := firstTierPriceUSD(tc.sku)
			if (got-tc.want) > 1e-9 || (tc.want-got) > 1e-9 {
				t.Fatalf("firstTierPriceUSD()=%v want %v", got, tc.want)
			}
		})
	}
}

func Test_parseProjects(t *testing.T) {
	cases := []struct {
		name      string
		projectID string
		csv       string
		want      []string
	}{
		{"only projectID", "my-proj", "", []string{"my-proj"}},
		{"csv overrides", "my-proj", "a,b , c", []string{"a", "b", "c"}},
		{"empty both", "", "", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := common.ParseProjects(tc.projectID, tc.csv)
			if len(got) != len(tc.want) {
				t.Fatalf("len(parseProjects)=%d want %d (got=%v)", len(got), len(tc.want), got)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("parseProjects[%d]=%q want %q (full=%v)", i, got[i], tc.want[i], got)
				}
			}
		})
	}
}
