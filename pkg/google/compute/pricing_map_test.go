package compute

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/genproto/googleapis/type/money"
	"os"
	"testing"

	"cloud.google.com/go/billing/apiv1/billingpb"
)

func Test_getDataFromSku(t *testing.T) {
	tests := map[string]struct {
		description       string
		serviceRegions    []string
		price             int32
		wantParsedSkuData *ParsedSkuData
		wantError         error
	}{
		"Core": {
			description:       "G2 Instance Core running in Sao Paulo",
			serviceRegions:    []string{"europe-west1"},
			price:             12,
			wantParsedSkuData: NewParsedSkuData("europe-west1", OnDemand, 12, "g2", Cpu),
			wantError:         nil,
		},
		"Ram": {
			description:       "G2 Instance Ram running in Belgium",
			serviceRegions:    []string{"europe-west1"},
			price:             12,
			wantParsedSkuData: NewParsedSkuData("europe-west1", OnDemand, 12, "g2", Ram),
			wantError:         nil,
		},
		"Ram N1": {
			description:       "N1 Predefined Instance Ram running in Zurich",
			serviceRegions:    []string{"europe-west1"},
			price:             12,
			wantParsedSkuData: NewParsedSkuData("europe-west1", OnDemand, 12, "n1", Ram),
			wantError:         nil,
		},
		"Amd": {
			description:       "N2D AMD Instance Ram running in Israel",
			serviceRegions:    []string{"europe-west1"},
			price:             12,
			wantParsedSkuData: NewParsedSkuData("europe-west1", OnDemand, 12, "n2d", Ram),
			wantError:         nil,
		},
		"Compute optimized": {
			description:       "Compute optimized Instance Core running in Dallas",
			serviceRegions:    []string{"europe-west1"},
			price:             12,
			wantParsedSkuData: NewParsedSkuData("europe-west1", OnDemand, 12, "c2", Cpu),
			wantError:         nil,
		},
		"Compute optimized Spot": {
			description:       "Spot Preemptible Compute optimized Ram running in Montreal",
			serviceRegions:    []string{"europe-west1"},
			price:             12,
			wantParsedSkuData: NewParsedSkuData("europe-west1", Spot, 12, "c2", Ram),
			wantError:         nil,
		},
		"3 word region": {
			description:       "Spot Preemptible E2 Instance Core running in Salt Lake City",
			serviceRegions:    []string{"europe-west1"},
			price:             12,
			wantParsedSkuData: NewParsedSkuData("europe-west1", Spot, 12, "e2", Cpu),
			wantError:         nil,
		},
		"Ignore GPU": {
			description:       "Nvidia L4 GPU attached to Spot Preemptible VMs running in Hong Kong",
			serviceRegions:    []string{"europe-west1"},
			price:             12,
			wantParsedSkuData: nil,
			wantError:         SkuNotRelevant,
		},
		"Ignore Network": {
			description:       "Network Internet Egress from Israel to South America",
			serviceRegions:    []string{"europe-west1"},
			price:             12,
			wantParsedSkuData: nil,
			wantError:         SkuNotRelevant,
		},
		"Ignore Sole Tenancy": {
			description:       "C3 Sole Tenancy Instance Ram running in Turin",
			serviceRegions:    []string{"europe-west1"},
			price:             12,
			wantParsedSkuData: nil,
			wantError:         SkuNotRelevant,
		},
		"Ignore Extreme PD Capacity": {
			description:       "Extreme PD Capacity in Las Vegas",
			serviceRegions:    []string{"europe-west1"},
			price:             12,
			wantParsedSkuData: nil,
			wantError:         SkuNotRelevant,
		},
		"Ignore Storage PD": {
			description:       "Storage PD Capacity in Seoul",
			serviceRegions:    []string{"europe-west1"},
			price:             12,
			wantParsedSkuData: nil,
			wantError:         SkuNotRelevant,
		},
		"Ignore Cloud Interconnect": {
			description:       "Cloud Interconnect - Egress traffic Asia Pacific",
			serviceRegions:    []string{"europe-west1"},
			price:             12,
			wantParsedSkuData: nil,
			wantError:         SkuNotRelevant,
		},
		"Ignore Commitment": {
			description:       "Commitment v1: Cpu in Montreal for 1 Year",
			serviceRegions:    []string{"europe-west1"},
			price:             12,
			wantParsedSkuData: nil,
			wantError:         SkuNotRelevant,
		},
		"Ignore Custom": {
			description:       "Spot Preemptible Custom Instance Core running in Dammam",
			serviceRegions:    []string{"europe-west1"},
			price:             12,
			wantParsedSkuData: nil,
			wantError:         SkuNotRelevant,
		},
		"Ignore Micro": {
			description:       "Spot Preemptible Micro Instance with burstable CPU running in EMEA",
			serviceRegions:    []string{"europe-west1"},
			price:             12,
			wantParsedSkuData: nil,
			wantError:         SkuNotRelevant,
		},
		"Ignore Small": {
			description:       "Spot Preemptible Small Instance with 1 VCPU running in Paris",
			serviceRegions:    []string{"europe-west1"},
			price:             12,
			wantParsedSkuData: nil,
			wantError:         SkuNotRelevant,
		},
		"Memory Optimized": {
			description:       "Memory-optimized Instance Core running in Zurich",
			serviceRegions:    []string{"europe-west1"},
			price:             12,
			wantParsedSkuData: nil,
			wantError:         SkuNotRelevant,
		},
	}
	for name, tt := range tests {
		sku := &billingpb.Sku{
			Description:    tt.description,
			ServiceRegions: tt.serviceRegions,
			PricingInfo: []*billingpb.PricingInfo{{
				PricingExpression: &billingpb.PricingExpression{
					TieredRates: []*billingpb.PricingExpression_TierRate{{
						UnitPrice: &money.Money{
							Nanos: tt.price}}}}}},
		}
		t.Run(name, func(t *testing.T) {
			gotParsedSkuData, gotErr := getDataFromSku(sku)
			if !cmp.Equal(gotParsedSkuData, tt.wantParsedSkuData) {
				t.Errorf("getDataFromSku() = %v, wantParsedSkuData %v", gotParsedSkuData, tt.wantParsedSkuData)
			}
			if !errors.Is(gotErr, tt.wantError) {
				t.Errorf("getDataFromSku() = %v, wantErr %v", gotErr, tt.wantError)
			}
		})
	}
}

func Test_parseAllProducts(t *testing.T) {
	t.Skip("Local only test. Comment this line to execute test.")
	file, err := os.Open("testdata/all-products.json")
	if err != nil {
		fmt.Println("Error opening file:", err)
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
	counter := 0
	for _, sku := range pricing {
		_, err := getDataFromSku(sku)
		if errors.Is(SkuNotParsable, err) {
			fmt.Printf("Not parsable yet: %v\n", sku.Description)
			counter++
		}
		if errors.Is(PricingDataIsOff, err) {
			fmt.Printf("Pricing is off: %v\n", sku.Description)
		}
	}
	fmt.Printf("%v SKU weren't parsable", counter)
}
