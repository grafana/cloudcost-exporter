package ec2

import (
	"context"
	"time"

	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	elbTypes "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	pricingTypes "github.com/aws/aws-sdk-go-v2/service/pricing/types"
	rdsTypes "github.com/aws/aws-sdk-go-v2/service/rds/types"
	"github.com/grafana/cloudcost-exporter/pkg/aws/client"
	"github.com/prometheus/client_golang/prometheus"
)

type storageClientMock struct {
	prices []string
	err    error
}

func (m *storageClientMock) ListStoragePrices(ctx context.Context, region string) ([]string, error) {
	return m.prices, m.err
}

func (m *storageClientMock) GetBillingData(ctx context.Context, startDate time.Time, endDate time.Time) (*client.BillingData, error) {
	panic("not implemented")
}

func (m *storageClientMock) DescribeRegions(ctx context.Context, allRegions bool) ([]ec2Types.Region, error) {
	panic("not implemented")
}

func (m *storageClientMock) ListComputeInstances(ctx context.Context) ([]ec2Types.Reservation, error) {
	panic("not implemented")
}

func (m *storageClientMock) ListEBSVolumes(ctx context.Context) ([]ec2Types.Volume, error) {
	panic("not implemented")
}

func (m *storageClientMock) ListSpotPrices(ctx context.Context) ([]ec2Types.SpotPrice, error) {
	panic("not implemented")
}

func (m *storageClientMock) ListOnDemandPrices(ctx context.Context, region string) ([]string, error) {
	panic("not implemented")
}

func (m *storageClientMock) ListEC2ServicePrices(ctx context.Context, region string, filters []pricingTypes.Filter) ([]string, error) {
	panic("not implemented")
}

func (m *storageClientMock) ListVPCServicePrices(ctx context.Context, region string, filters []pricingTypes.Filter) ([]string, error) {
	panic("not implemented")
}

func (m *storageClientMock) ListELBPrices(ctx context.Context, region string) ([]string, error) {
	panic("not implemented")
}

func (m *storageClientMock) DescribeLoadBalancers(ctx context.Context) ([]elbTypes.LoadBalancer, error) {
	panic("not implemented")
}

func (m *storageClientMock) ListRDSInstances(ctx context.Context) ([]rdsTypes.DBInstance, error) {
	panic("not implemented")
}

func (m *storageClientMock) GetRDSUnitData(ctx context.Context, instType, region, deploymentOption, engineCode, isOutpost string) (string, error) {
	panic("not implemented")
}

func (m *storageClientMock) Metrics() []prometheus.Collector {
	panic("not implemented")
}
