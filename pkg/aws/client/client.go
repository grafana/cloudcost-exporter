package client

import (
	"context"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	elbTypes "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	pricingTypes "github.com/aws/aws-sdk-go-v2/service/pricing/types"
	rdsTypes "github.com/aws/aws-sdk-go-v2/service/rds/types"
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
	ListVPCServicePrices(ctx context.Context, region string, filters []pricingTypes.Filter) ([]string, error)
	ListELBPrices(ctx context.Context, region string) ([]string, error)
	DescribeLoadBalancers(ctx context.Context) ([]elbTypes.LoadBalancer, error)
	ListRDSInstances(ctx context.Context) ([]rdsTypes.DBInstance, error)
	GetRDSUnitData(ctx context.Context, instType, region, deploymentOption, engineCode, isOutpost string) (string, error)

	// TODO: Break out Metrics into an independent interface
	Metrics() []prometheus.Collector
}
