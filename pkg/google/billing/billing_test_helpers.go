package billing

import (
	"context"

	"cloud.google.com/go/billing/apiv1/billingpb"
	"google.golang.org/genproto/googleapis/type/money"
)

type FakeCloudCatalogServer struct {
	billingpb.UnimplementedCloudCatalogServer
}

func (s *FakeCloudCatalogServer) ListServices(ctx context.Context, req *billingpb.ListServicesRequest) (*billingpb.ListServicesResponse, error) {
	return &billingpb.ListServicesResponse{
		Services: []*billingpb.Service{
			{
				DisplayName: "Compute Engine",
				Name:        "compute-engine",
			},
		},
	}, nil
}

func (s *FakeCloudCatalogServer) ListSkus(ctx context.Context, req *billingpb.ListSkusRequest) (*billingpb.ListSkusResponse, error) {
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
			{
				Name:           "us-east1 as part of us-central-1 compute",
				Description:    "N2 Predefined Instance Core running in Americas",
				ServiceRegions: []string{"us-central-1", "us-east1"},
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
				Name:           "us-east1 as part of us-central-1 memory",
				Description:    "N2 Predefined Instance Ram running in Americas",
				ServiceRegions: []string{"us-central-1", "us-east1"},
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

type FakeCloudCatalogServerSlimResults struct {
	billingpb.UnimplementedCloudCatalogServer
}

func (s *FakeCloudCatalogServerSlimResults) ListServices(ctx context.Context, req *billingpb.ListServicesRequest) (*billingpb.ListServicesResponse, error) {
	return &billingpb.ListServicesResponse{
		Services: []*billingpb.Service{
			{
				DisplayName: "Compute Engine",
				Name:        "compute-engine",
			},
		},
	}, nil
}

func (s *FakeCloudCatalogServerSlimResults) ListSkus(ctx context.Context, req *billingpb.ListSkusRequest) (*billingpb.ListSkusResponse, error) {
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
