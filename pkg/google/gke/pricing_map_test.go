package gke

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/grafana/cloudcost-exporter/pkg/google/client"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/type/money"

	"cloud.google.com/go/billing/apiv1/billingpb"

	"github.com/grafana/cloudcost-exporter/pkg/utils"
)

func TestStructuredPricingMap_GetCostOfInstance(t *testing.T) {
	for _, tc := range []struct {
		name             string
		pm               PricingMap
		ms               *client.MachineSpec
		expectedCPUPrice float64
		expectedRAMPRice float64
		expectedError    error
	}{
		{
			name:          "regions is nil",
			expectedError: ErrRegionNotFound,
		},
		{
			name:          "nil machine spec",
			pm:            PricingMap{compute: map[string]*FamilyPricing{"": {}}},
			expectedError: ErrRegionNotFound,
		},
		{
			name:          "region not found",
			pm:            PricingMap{compute: map[string]*FamilyPricing{"": {}}},
			ms:            &client.MachineSpec{Region: "missing region"},
			expectedError: ErrRegionNotFound,
		},
		{
			name:          "family type not found",
			pm:            PricingMap{compute: map[string]*FamilyPricing{"region": {}}},
			ms:            &client.MachineSpec{Region: "region"},
			expectedError: ErrFamilyTypeNotFound,
		},
		{
			name: "on-demand",
			pm: PricingMap{
				compute: map[string]*FamilyPricing{
					"region": {
						Family: map[string]*PriceTiers{
							"family": {
								OnDemand: Prices{
									Cpu: 1,
									Ram: 2,
								},
							},
						},
					},
				},
			},
			ms: &client.MachineSpec{
				Region: "region",
				Family: "family",
			},
			expectedCPUPrice: 1,
			expectedRAMPRice: 2,
		},
		{
			name: "spot",
			pm: PricingMap{
				compute: map[string]*FamilyPricing{
					"region": {
						Family: map[string]*PriceTiers{
							"family": {
								Spot: Prices{
									Cpu: 3,
									Ram: 4,
								},
							},
						},
					},
				},
			},
			expectedCPUPrice: 3,
			expectedRAMPRice: 4,
			ms: &client.MachineSpec{
				Region:       "region",
				Family:       "family",
				SpotInstance: true,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, r, err := tc.pm.GetCostOfInstance(tc.ms)
			if tc.expectedError != nil {
				require.ErrorIs(t, err, tc.expectedError)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.expectedCPUPrice, c, "cpu price mismatch")
			require.Equal(t, tc.expectedRAMPRice, r, "ram price mismatch")
		})
	}
}

func TestPricingMapParseSkus(t *testing.T) {
	for _, tc := range []struct {
		name               string
		skus               []*billingpb.Sku
		expectedPricingMap *PricingMap
		expectedError      error
	}{
		{
			name: "empty sku, empty pricing map",
			skus: []*billingpb.Sku{{}},
			expectedPricingMap: &PricingMap{
				compute: map[string]*FamilyPricing{},
				storage: map[string]*StoragePricing{},
			},
		},
		{
			name:          "nil sku, bubble-up error",
			skus:          []*billingpb.Sku{nil},
			expectedError: ErrSkuIsNil,
		},
		{
			name: "sku not relevant",
			skus: []*billingpb.Sku{{
				Description: "Nvidia L4 GPU attached to Spot Preemptible VMs running in Hong Kong",
				PricingInfo: []*billingpb.PricingInfo{{
					PricingExpression: &billingpb.PricingExpression{
						TieredRates: []*billingpb.PricingExpression_TierRate{{
							UnitPrice: &money.Money{
								Nanos: 1e9,
							},
						}},
					},
				}},
			}},
			expectedPricingMap: &PricingMap{
				compute: map[string]*FamilyPricing{},
				storage: map[string]*StoragePricing{},
			},
		},
		{
			name: "sku not parsable",
			skus: []*billingpb.Sku{{
				Description: "No more guava's allowed in the codebase",
				PricingInfo: []*billingpb.PricingInfo{{
					PricingExpression: &billingpb.PricingExpression{
						TieredRates: []*billingpb.PricingExpression_TierRate{{
							UnitPrice: &money.Money{
								Nanos: 1e9,
							},
						}},
					},
				}},
			}},
			expectedPricingMap: &PricingMap{
				compute: map[string]*FamilyPricing{},
				storage: map[string]*StoragePricing{},
			},
		},
		{
			name: "on-demand cpu",
			skus: []*billingpb.Sku{{
				Description:    "G2 Instance Core running in Sao Paulo",
				ServiceRegions: []string{"europe-west1"},
				PricingInfo: []*billingpb.PricingInfo{{
					PricingExpression: &billingpb.PricingExpression{
						TieredRates: []*billingpb.PricingExpression_TierRate{{
							UnitPrice: &money.Money{
								Nanos: 1e9,
							},
						}},
					},
				}},
			}},
			expectedPricingMap: &PricingMap{
				compute: map[string]*FamilyPricing{
					"europe-west1": {
						Family: map[string]*PriceTiers{
							"g2": {
								OnDemand: Prices{
									Cpu: 1,
								},
							},
						},
					},
				},
				storage: map[string]*StoragePricing{},
			},
		},
		{
			name: "on-demand cpu - multiple regions",
			skus: []*billingpb.Sku{{
				Description:    "G2 Instance Core running in Sao Paulo",
				ServiceRegions: []string{"us-central1", "us-east1"},
				PricingInfo: []*billingpb.PricingInfo{{
					PricingExpression: &billingpb.PricingExpression{
						TieredRates: []*billingpb.PricingExpression_TierRate{{
							UnitPrice: &money.Money{
								Nanos: 1e9,
							},
						}},
					},
				}},
			}},
			expectedPricingMap: &PricingMap{
				compute: map[string]*FamilyPricing{
					"us-central1": {
						Family: map[string]*PriceTiers{
							"g2": {
								OnDemand: Prices{
									Cpu: 1,
								},
							},
						},
					},
					"us-east1": {
						Family: map[string]*PriceTiers{
							"g2": {
								OnDemand: Prices{
									Cpu: 1,
								},
							},
						},
					},
				},
				storage: map[string]*StoragePricing{},
			},
		},
		{
			name: "spot cpu c4a",
			skus: []*billingpb.Sku{{
				Description:    "Spot Preemptible C4A Arm Instance Core running in Belgium",
				ServiceRegions: []string{"europe-west1"},
				PricingInfo: []*billingpb.PricingInfo{{
					PricingExpression: &billingpb.PricingExpression{
						TieredRates: []*billingpb.PricingExpression_TierRate{{
							UnitPrice: &money.Money{
								Nanos: 1e9,
							},
						}},
					},
				}},
			}},
			expectedPricingMap: &PricingMap{
				compute: map[string]*FamilyPricing{
					"europe-west1": {
						Family: map[string]*PriceTiers{
							"c4a": {
								Spot: Prices{
									Cpu: 1,
								},
							},
						},
					},
				},
				storage: map[string]*StoragePricing{},
			},
		},
		{
			name: "spot c4a ram",
			skus: []*billingpb.Sku{{
				Description:    "Spot Preemptible C4A Arm Instance Ram running in Belgium",
				ServiceRegions: []string{"europe-west1"},
				PricingInfo: []*billingpb.PricingInfo{{
					PricingExpression: &billingpb.PricingExpression{
						TieredRates: []*billingpb.PricingExpression_TierRate{{
							UnitPrice: &money.Money{
								Nanos: 1e9,
							},
						}},
					},
				}},
			}},
			expectedPricingMap: &PricingMap{
				compute: map[string]*FamilyPricing{
					"europe-west1": {
						Family: map[string]*PriceTiers{
							"c4a": {
								Spot: Prices{
									Ram: 1,
								},
							},
						},
					},
				},
				storage: map[string]*StoragePricing{},
			},
		},
		{
			name: "on-demand cpu c4a",
			skus: []*billingpb.Sku{{
				Description:    "C4A Arm Instance Core running in Belgium",
				ServiceRegions: []string{"europe-west1"},
				PricingInfo: []*billingpb.PricingInfo{{
					PricingExpression: &billingpb.PricingExpression{
						TieredRates: []*billingpb.PricingExpression_TierRate{{
							UnitPrice: &money.Money{
								Nanos: 1e9,
							},
						}},
					},
				}},
			}},
			expectedPricingMap: &PricingMap{
				compute: map[string]*FamilyPricing{
					"europe-west1": {
						Family: map[string]*PriceTiers{
							"c4a": {
								OnDemand: Prices{
									Cpu: 1,
								},
							},
						},
					},
				},
				storage: map[string]*StoragePricing{},
			},
		},
		{
			name: "c4a ram",
			skus: []*billingpb.Sku{{
				Description:    "C4A Arm Instance Ram running in Belgium",
				ServiceRegions: []string{"europe-west1"},
				PricingInfo: []*billingpb.PricingInfo{{
					PricingExpression: &billingpb.PricingExpression{
						TieredRates: []*billingpb.PricingExpression_TierRate{{
							UnitPrice: &money.Money{
								Nanos: 1e9,
							},
						}},
					},
				}},
			}},
			expectedPricingMap: &PricingMap{
				compute: map[string]*FamilyPricing{
					"europe-west1": {
						Family: map[string]*PriceTiers{
							"c4a": {
								OnDemand: Prices{
									Ram: 1,
								},
							},
						},
					},
				},
				storage: map[string]*StoragePricing{},
			},
		},
		{
			name: "on-demand ram",
			skus: []*billingpb.Sku{{
				Description:    "G2 Instance Ram running in Belgium",
				ServiceRegions: []string{"europe-west1"},
				PricingInfo: []*billingpb.PricingInfo{{
					PricingExpression: &billingpb.PricingExpression{
						TieredRates: []*billingpb.PricingExpression_TierRate{{
							UnitPrice: &money.Money{
								Nanos: 1e9,
							},
						}},
					},
				}},
			}},
			expectedPricingMap: &PricingMap{
				compute: map[string]*FamilyPricing{
					"europe-west1": {
						Family: map[string]*PriceTiers{
							"g2": {
								OnDemand: Prices{
									Ram: 1,
								},
							},
						},
					},
				},
				storage: map[string]*StoragePricing{},
			},
		},
		{
			name: "spot cpu",
			skus: []*billingpb.Sku{{
				Description:    "Spot Preemptible E2 Instance Core running in Salt Lake City",
				ServiceRegions: []string{"europe-west1"},
				PricingInfo: []*billingpb.PricingInfo{{
					PricingExpression: &billingpb.PricingExpression{
						TieredRates: []*billingpb.PricingExpression_TierRate{{
							UnitPrice: &money.Money{
								Nanos: 1e9,
							},
						}},
					},
				}},
			}},
			expectedPricingMap: &PricingMap{
				compute: map[string]*FamilyPricing{
					"europe-west1": {
						Family: map[string]*PriceTiers{
							"e2": {
								Spot: Prices{
									Cpu: 1,
								},
							},
						},
					},
				},
				storage: map[string]*StoragePricing{},
			},
		},
		{
			name: "Standard PD",
			skus: []*billingpb.Sku{{
				Description:    "Storage PD Capacity",
				Category:       &billingpb.Category{ResourceFamily: "Storage"},
				ServiceRegions: []string{"europe-west1"},
				PricingInfo: []*billingpb.PricingInfo{{
					PricingExpression: &billingpb.PricingExpression{
						TieredRates: []*billingpb.PricingExpression_TierRate{{
							UnitPrice: &money.Money{
								Nanos: 0.0,
							},
						}, {
							UnitPrice: &money.Money{
								Nanos: 1e9,
							},
						}},
					},
				}},
			}},
			expectedPricingMap: &PricingMap{
				storage: map[string]*StoragePricing{
					"europe-west1": {
						Storage: map[string]*StoragePrices{
							"pd-standard": {
								ProvisionedSpaceGiB: 1.0 / utils.HoursInMonth,
							},
						},
					},
				},
				compute: map[string]*FamilyPricing{},
			},
		},
		{
			name: "SSD Pricing",
			skus: []*billingpb.Sku{{
				Description:    "SSD backed PD Capacity",
				Category:       &billingpb.Category{ResourceFamily: "Storage"},
				ServiceRegions: []string{"europe-west1"},
				PricingInfo: []*billingpb.PricingInfo{{
					PricingExpression: &billingpb.PricingExpression{
						TieredRates: []*billingpb.PricingExpression_TierRate{{
							UnitPrice: &money.Money{
								Nanos: 1e9,
							},
						}},
					},
				}},
			}},
			expectedPricingMap: &PricingMap{
				storage: map[string]*StoragePricing{
					"europe-west1": {
						Storage: map[string]*StoragePrices{
							"pd-ssd": {
								ProvisionedSpaceGiB: 1.0 / utils.HoursInMonth,
							},
						},
					},
				},
				compute: map[string]*FamilyPricing{},
			},
		},
		{
			name: "Balanced Disk Pricing",
			skus: []*billingpb.Sku{{
				Description:    "Balanced PD Capacity",
				Category:       &billingpb.Category{ResourceFamily: "Storage"},
				ServiceRegions: []string{"europe-west1"},
				PricingInfo: []*billingpb.PricingInfo{{
					PricingExpression: &billingpb.PricingExpression{
						TieredRates: []*billingpb.PricingExpression_TierRate{{
							UnitPrice: &money.Money{
								Nanos: 1e9,
							},
						}},
					},
				}},
			}, {
				Description:    "Regional Balanced PD Capacity",
				Category:       &billingpb.Category{ResourceFamily: "Storage"},
				ServiceRegions: []string{"europe-west1"},
				PricingInfo: []*billingpb.PricingInfo{{
					PricingExpression: &billingpb.PricingExpression{
						TieredRates: []*billingpb.PricingExpression_TierRate{{
							UnitPrice: &money.Money{
								Nanos: 1e9 * 2,
							},
						}},
					},
				}},
			}},
			expectedPricingMap: &PricingMap{
				storage: map[string]*StoragePricing{
					"europe-west1": {
						Storage: map[string]*StoragePrices{
							"pd-balanced": {
								ProvisionedSpaceGiB: 1.0 / utils.HoursInMonth,
							},
						},
					},
				},
				compute: map[string]*FamilyPricing{},
			},
		},
		{
			name: "Extreme Disk Pricing",
			skus: []*billingpb.Sku{{
				Description:    "Extreme PD Capacity",
				Category:       &billingpb.Category{ResourceFamily: "Storage"},
				ServiceRegions: []string{"europe-west1"},
				PricingInfo: []*billingpb.PricingInfo{{
					PricingExpression: &billingpb.PricingExpression{
						TieredRates: []*billingpb.PricingExpression_TierRate{{
							UnitPrice: &money.Money{
								Nanos: 1e9,
							},
						}},
					},
				}},
			}},
			expectedPricingMap: &PricingMap{
				storage: map[string]*StoragePricing{
					"europe-west1": {
						Storage: map[string]*StoragePrices{
							"pd-extreme": {
								ProvisionedSpaceGiB: 1.0 / utils.HoursInMonth,
							},
						},
					},
				},
				compute: map[string]*FamilyPricing{},
			},
		},
		{
			name: "HyperDisk Pricing",
			skus: []*billingpb.Sku{{
				Description:    "Hyperdisk Balanced Capacity",
				Category:       &billingpb.Category{ResourceFamily: "Storage"},
				ServiceRegions: []string{"europe-west1"},
				PricingInfo: []*billingpb.PricingInfo{{
					PricingExpression: &billingpb.PricingExpression{
						TieredRates: []*billingpb.PricingExpression_TierRate{{
							UnitPrice: &money.Money{
								Nanos: 1e9,
							},
						}},
					},
				}},
			}},
			expectedPricingMap: &PricingMap{
				storage: map[string]*StoragePricing{
					"europe-west1": {
						Storage: map[string]*StoragePrices{
							"hyperdisk-balanced": {
								ProvisionedSpaceGiB: 1.0 / utils.HoursInMonth,
							},
						},
					},
				},
				compute: map[string]*FamilyPricing{},
			},
		},
		{
			name: "us-east-4 region with many skus",
			skus: []*billingpb.Sku{{
				Description:    "SSD backed PD Capacity",
				Category:       &billingpb.Category{ResourceFamily: "Storage"},
				ServiceRegions: []string{"us-east4"},
				PricingInfo: []*billingpb.PricingInfo{{
					PricingExpression: &billingpb.PricingExpression{
						TieredRates: []*billingpb.PricingExpression_TierRate{{
							UnitPrice: &money.Money{
								Nanos: 187000000,
							},
						}},
					},
				}},
			},
				{
					Description:    "Regional SSD backed PD Capacity",
					Category:       &billingpb.Category{ResourceFamily: "Storage"},
					ServiceRegions: []string{"us-east4"},
					PricingInfo: []*billingpb.PricingInfo{{
						PricingExpression: &billingpb.PricingExpression{
							TieredRates: []*billingpb.PricingExpression_TierRate{{
								UnitPrice: &money.Money{
									Nanos: 187000000 * 2,
								},
							}},
						},
					}},
				},
			},
			expectedPricingMap: &PricingMap{
				storage: map[string]*StoragePricing{
					"us-east4": {
						Storage: map[string]*StoragePrices{
							"pd-ssd": {
								ProvisionedSpaceGiB: 187000000 * 1e-9 / utils.HoursInMonth,
							},
						},
					},
				},
				compute: map[string]*FamilyPricing{},
			},
		},
		{
			name: "spot ram",
			skus: []*billingpb.Sku{{
				Description:    "Spot Preemptible Compute optimized Ram running in Montreal",
				ServiceRegions: []string{"europe-west1"},
				PricingInfo: []*billingpb.PricingInfo{{
					PricingExpression: &billingpb.PricingExpression{
						TieredRates: []*billingpb.PricingExpression_TierRate{{
							UnitPrice: &money.Money{
								Nanos: 1e9,
							},
						}},
					},
				}},
			}},
			expectedPricingMap: &PricingMap{
				compute: map[string]*FamilyPricing{
					"europe-west1": {
						Family: map[string]*PriceTiers{
							"c2": {
								Spot: Prices{
									Ram: 1,
								},
							},
						},
					},
				},
				storage: map[string]*StoragePricing{},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pricingMap := &PricingMap{
				compute: make(map[string]*FamilyPricing),
				storage: make(map[string]*StoragePricing),
			}
			err := pricingMap.ParseSkus(tc.skus)
			if tc.expectedError != nil {
				require.EqualError(t, err, tc.expectedError.Error())
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.expectedPricingMap, pricingMap)
		})
	}
}

func Test_getDataFromSku_sadPaths(t *testing.T) {
	_, err := getDataFromSku(nil)
	require.ErrorIs(t, err, ErrSkuIsNil)

	_, err = getDataFromSku(&billingpb.Sku{})
	require.ErrorIs(t, err, ErrSkuNotParsable)

	_, err = getDataFromSku(&billingpb.Sku{
		Description: "Nvidia L4 GPU attached to Spot Preemptible VMs running in Hong Kong",
	})
	require.ErrorIs(t, err, ErrSkuNotRelevant)
}

func Test_getDataFromSku(t *testing.T) {
	tests := map[string]struct {
		description       string
		serviceCompute    []string
		price             int32
		wantParsedSkuData []*ParsedSkuData
		wantError         error
	}{
		"Core": {
			description:       "G2 Instance Core running in Sao Paulo",
			serviceCompute:    []string{"europe-west1"},
			price:             12,
			wantParsedSkuData: []*ParsedSkuData{NewParsedSkuData("europe-west1", OnDemand, 12, "g2", Cpu)},
			wantError:         nil,
		},
		"Ram": {
			description:       "G2 Instance Ram running in Belgium",
			serviceCompute:    []string{"europe-west1"},
			price:             12,
			wantParsedSkuData: []*ParsedSkuData{NewParsedSkuData("europe-west1", OnDemand, 12, "g2", Ram)},
			wantError:         nil,
		},
		"Ram N1": {
			description:       "N1 Predefined Instance Ram running in Zurich",
			serviceCompute:    []string{"europe-west1"},
			price:             12,
			wantParsedSkuData: []*ParsedSkuData{NewParsedSkuData("europe-west1", OnDemand, 12, "n1", Ram)},
			wantError:         nil,
		},
		"Amd": {
			description:       "N2D AMD Instance Ram running in Israel",
			serviceCompute:    []string{"europe-west1"},
			price:             12,
			wantParsedSkuData: []*ParsedSkuData{NewParsedSkuData("europe-west1", OnDemand, 12, "n2d", Ram)},
			wantError:         nil,
		},
		"Compute optimized": {
			description:       "Compute optimized Instance Core running in Dallas",
			serviceCompute:    []string{"europe-west1"},
			price:             12,
			wantParsedSkuData: []*ParsedSkuData{NewParsedSkuData("europe-west1", OnDemand, 12, "c2", Cpu)},
			wantError:         nil,
		},
		"Compute optimized Spot": {
			description:       "Spot Preemptible Compute optimized Ram running in Montreal",
			serviceCompute:    []string{"europe-west1"},
			price:             12,
			wantParsedSkuData: []*ParsedSkuData{NewParsedSkuData("europe-west1", Spot, 12, "c2", Ram)},
			wantError:         nil,
		},
		"3 word region": {
			description:       "Spot Preemptible E2 Instance Core running in Salt Lake City",
			serviceCompute:    []string{"europe-west1"},
			price:             12,
			wantParsedSkuData: []*ParsedSkuData{NewParsedSkuData("europe-west1", Spot, 12, "e2", Cpu)},
			wantError:         nil,
		},
		"Ignore GPU": {
			description:       "Nvidia L4 GPU attached to Spot Preemptible VMs running in Hong Kong",
			serviceCompute:    []string{"europe-west1"},
			price:             12,
			wantParsedSkuData: nil,
			wantError:         ErrSkuNotRelevant,
		},
		"Ignore Network": {
			description:       "Network Internet Egress from Israel to South America",
			serviceCompute:    []string{"europe-west1"},
			price:             12,
			wantParsedSkuData: nil,
			wantError:         ErrSkuNotRelevant,
		},
		"Ignore Sole Tenancy": {
			description:       "C3 Sole Tenancy Instance Ram running in Turin",
			serviceCompute:    []string{"europe-west1"},
			price:             12,
			wantParsedSkuData: nil,
			wantError:         ErrSkuNotRelevant,
		},
		"Ignore Cloud Interconnect": {
			description:       "Cloud Interconnect - Egress traffic Asia Pacific",
			serviceCompute:    []string{"europe-west1"},
			price:             12,
			wantParsedSkuData: nil,
			wantError:         ErrSkuNotRelevant,
		},
		"Ignore Commitment": {
			description:       "Commitment v1: Cpu in Montreal for 1 Year",
			serviceCompute:    []string{"europe-west1"},
			price:             12,
			wantParsedSkuData: nil,
			wantError:         ErrSkuNotRelevant,
		},
		"Ignore Custom": {
			description:       "Spot Preemptible Custom Instance Core running in Dammam",
			serviceCompute:    []string{"europe-west1"},
			price:             12,
			wantParsedSkuData: nil,
			wantError:         ErrSkuNotRelevant,
		},
		"Ignore Micro": {
			description:       "Spot Preemptible Micro Instance with burstable CPU running in EMEA",
			serviceCompute:    []string{"europe-west1"},
			price:             12,
			wantParsedSkuData: nil,
			wantError:         ErrSkuNotRelevant,
		},
		"Ignore Small": {
			description:       "Spot Preemptible Small Instance with 1 VCPU running in Paris",
			serviceCompute:    []string{"europe-west1"},
			price:             12,
			wantParsedSkuData: nil,
			wantError:         ErrSkuNotRelevant,
		},
		"Memory Optimized": {
			description:       "Memory-optimized Instance Core running in Zurich",
			serviceCompute:    []string{"europe-west1"},
			price:             12,
			wantParsedSkuData: nil,
			wantError:         ErrSkuNotRelevant,
		},
		"Not parsable": {
			description: "No more guava's allowed in the codebase",
			wantError:   ErrSkuNotParsable,
		},
	}
	for name, tt := range tests {
		sku := &billingpb.Sku{
			Description:    tt.description,
			ServiceRegions: tt.serviceCompute,
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
		if errors.Is(ErrSkuNotParsable, err) {
			fmt.Printf("Not parsable yet: %v\n", sku.Description)
			counter++
		}
		if errors.Is(ErrPricingDataIsOff, err) {
			fmt.Printf("Pricing is off: %v\n", sku.Description)
		}
	}
	fmt.Printf("%v SKU weren't parsable", counter)
}
