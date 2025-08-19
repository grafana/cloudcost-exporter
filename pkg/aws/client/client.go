package client

import (
	"context"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	pricingTypes "github.com/aws/aws-sdk-go-v2/service/pricing/types"
	"github.com/prometheus/client_golang/prometheus"
)

//go:generate mockgen -source=client.go -destination mocks/client.go

type Client interface {
	GetBillingData(ctx context.Context, startDate time.Time, endDate time.Time) (*BillingData, error)
	DescribeRegions(ctx context.Context, allRegions bool) ([]types.Region, error)
	ListComputeInstances(ctx context.Context) ([]types.Reservation, error)
	ListEBSVolumes(ctx context.Context) ([]types.Volume, error)
	ListSpotPrices(ctx context.Context) ([]types.SpotPrice, error)
	ListOnDemandPrices(ctx context.Context, region string) ([]string, error)
	ListStoragePrices(ctx context.Context, region string) ([]string, error)
	ListEC2ServicePrices(ctx context.Context, region string, filters []pricingTypes.Filter) ([]string, error)

	// TODO: Break out Metrics into an independent interface
	Metrics() []prometheus.Collector
}
