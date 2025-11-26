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

type mockClient struct {
	// Compute pricing fields
	ondemandPrices []string
	spotPrices     []ec2Types.SpotPrice
	ondemandErr    error
	spotErr        error

	// Storage pricing fields
	storagePrices []string
	storageErr    error
}

func (m *mockClient) ListOnDemandPrices(ctx context.Context, region string) ([]string, error) {
	return m.ondemandPrices, m.ondemandErr
}

func (m *mockClient) ListSpotPrices(ctx context.Context) ([]ec2Types.SpotPrice, error) {
	return m.spotPrices, m.spotErr
}

func (m *mockClient) ListStoragePrices(ctx context.Context, region string) ([]string, error) {
	return m.storagePrices, m.storageErr
}

func (m *mockClient) GetBillingData(ctx context.Context, startDate time.Time, endDate time.Time) (*client.BillingData, error) {
	panic("not implemented")
}

func (m *mockClient) DescribeRegions(ctx context.Context, allRegions bool) ([]ec2Types.Region, error) {
	panic("not implemented")
}

func (m *mockClient) ListComputeInstances(ctx context.Context) ([]ec2Types.Reservation, error) {
	panic("not implemented")
}

func (m *mockClient) ListEBSVolumes(ctx context.Context) ([]ec2Types.Volume, error) {
	panic("not implemented")
}

func (m *mockClient) ListEC2ServicePrices(ctx context.Context, region string, filters []pricingTypes.Filter) ([]string, error) {
	panic("not implemented")
}

func (m *mockClient) ListVPCServicePrices(ctx context.Context, region string, filters []pricingTypes.Filter) ([]string, error) {
	panic("not implemented")
}

func (m *mockClient) ListELBPrices(ctx context.Context, region string) ([]string, error) {
	panic("not implemented")
}

func (m *mockClient) DescribeLoadBalancers(ctx context.Context) ([]elbTypes.LoadBalancer, error) {
	panic("not implemented")
}

func (m *mockClient) ListRDSInstances(ctx context.Context) ([]rdsTypes.DBInstance, error) {
	panic("not implemented")
}

func (m *mockClient) GetRDSUnitData(ctx context.Context, instType, region, deploymentOption, engineCode, isOutpost string) (string, error) {
	panic("not implemented")
}

func (m *mockClient) Metrics() []prometheus.Collector {
	panic("not implemented")
}
