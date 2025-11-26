package client

import (
	"context"
	"net"
	"testing"

	billingv1 "cloud.google.com/go/billing/apiv1"
	"cloud.google.com/go/billing/apiv1/billingpb"
	"google.golang.org/api/option"
	"google.golang.org/genproto/googleapis/type/money"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type FakeCloudCatalogServer struct {
	billingpb.UnimplementedCloudCatalogServer
}

func (s *FakeCloudCatalogServer) ListServices(_ context.Context, req *billingpb.ListServicesRequest) (*billingpb.ListServicesResponse, error) {
	return &billingpb.ListServicesResponse{
		Services: []*billingpb.Service{
			{
				DisplayName: "Compute Engine",
				Name:        "compute-engine",
			},
		},
	}, nil
}

func (s *FakeCloudCatalogServer) ListSkus(_ context.Context, req *billingpb.ListSkusRequest) (*billingpb.ListSkusResponse, error) {
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
			{
				Name:           "standard-storage",
				Description:    "Storage PD Capacity",
				ServiceRegions: []string{"us-central1"},
				Category: &billingpb.Category{
					ResourceFamily: "Storage",
				},
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
			},
			{
				Name:           "SSD Storage",
				Description:    "SSD backed PD Capacity",
				ServiceRegions: []string{"us-east4"},
				Category: &billingpb.Category{
					ResourceFamily: "Storage",
				},
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
		},
	}, nil
}

type FakeCloudCatalogServerSlimResults struct {
	billingpb.UnimplementedCloudCatalogServer
}

func (s *FakeCloudCatalogServerSlimResults) ListServices(_ context.Context, req *billingpb.ListServicesRequest) (*billingpb.ListServicesResponse, error) {
	return &billingpb.ListServicesResponse{
		Services: []*billingpb.Service{
			{
				DisplayName: "Compute Engine",
				Name:        "compute-engine",
			},
		},
	}, nil
}

func (s *FakeCloudCatalogServerSlimResults) ListSkus(_ context.Context, req *billingpb.ListSkusRequest) (*billingpb.ListSkusResponse, error) {
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
				Name:           "standard-storage",
				Description:    "Storage PD Capacity",
				ServiceRegions: []string{"us-central1"},
				Category: &billingpb.Category{
					ResourceFamily: "Storage",
				},
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
			},
		},
	}, nil
}

// FakeCloudCatalogServerWithSKUs is a configurable fake billing server that allows
// customizing the service name and SKUs returned.
type FakeCloudCatalogServerWithSKUs struct {
	billingpb.UnimplementedCloudCatalogServer
	ServiceName string
	ServiceID   string
	Skus        []*billingpb.Sku
}

func (s *FakeCloudCatalogServerWithSKUs) ListServices(_ context.Context, _ *billingpb.ListServicesRequest) (*billingpb.ListServicesResponse, error) {
	serviceID := s.ServiceID
	if serviceID == "" {
		serviceID = "services/cloud-sql"
	}
	displayName := s.ServiceName
	if displayName == "" {
		displayName = "Cloud SQL"
	}
	return &billingpb.ListServicesResponse{
		Services: []*billingpb.Service{
			{
				Name:        serviceID,
				DisplayName: displayName,
			},
		},
	}, nil
}

func (s *FakeCloudCatalogServerWithSKUs) ListSkus(_ context.Context, _ *billingpb.ListSkusRequest) (*billingpb.ListSkusResponse, error) {
	return &billingpb.ListSkusResponse{
		Skus: s.Skus,
	}, nil
}

// NewTestBillingClient creates a billing client connected to a test gRPC server.
// The server is registered with the provided fake server implementation and will be
// cleaned up when the test completes.
func NewTestBillingClient(t *testing.T, server billingpb.CloudCatalogServer) *billingv1.CloudCatalogClient {
	t.Helper()

	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	gsrv := grpc.NewServer()
	billingpb.RegisterCloudCatalogServer(gsrv, server)
	go func() {
		if err := gsrv.Serve(l); err != nil {
			t.Errorf("failed to serve: %v", err)
		}
	}()
	t.Cleanup(gsrv.Stop)

	catalogClient, err := billingv1.NewCloudCatalogClient(context.Background(),
		option.WithEndpoint(l.Addr().String()),
		option.WithoutAuthentication(),
		option.WithGRPCDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())))
	if err != nil {
		t.Fatalf("failed to create billing client: %v", err)
	}

	return catalogClient
}
