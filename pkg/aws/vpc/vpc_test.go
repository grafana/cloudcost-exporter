package vpc

import (
	"context"
	"io"
	"os"
	"testing"
	"time"

	"log/slog"

	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	elbv2Types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	pricingTypes "github.com/aws/aws-sdk-go-v2/service/pricing/types"
	rdsTypes "github.com/aws/aws-sdk-go-v2/service/rds/types"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	awsclient "github.com/grafana/cloudcost-exporter/pkg/aws/client"
)

// MockClient for testing
type MockClient struct {
	mock.Mock
}

func (m *MockClient) GetBillingData(ctx context.Context, startDate, endDate time.Time) (*awsclient.BillingData, error) {
	args := m.Called(ctx, startDate, endDate)
	return args.Get(0).(*awsclient.BillingData), args.Error(1)
}

func (m *MockClient) DescribeRegions(ctx context.Context, allRegions bool) ([]ec2Types.Region, error) {
	args := m.Called(ctx, allRegions)
	return args.Get(0).([]ec2Types.Region), args.Error(1)
}

func (m *MockClient) ListComputeInstances(ctx context.Context) ([]ec2Types.Reservation, error) {
	args := m.Called(ctx)
	return args.Get(0).([]ec2Types.Reservation), args.Error(1)
}

func (m *MockClient) ListEBSVolumes(ctx context.Context) ([]ec2Types.Volume, error) {
	args := m.Called(ctx)
	return args.Get(0).([]ec2Types.Volume), args.Error(1)
}

func (m *MockClient) ListSpotPrices(ctx context.Context) ([]ec2Types.SpotPrice, error) {
	args := m.Called(ctx)
	return args.Get(0).([]ec2Types.SpotPrice), args.Error(1)
}

func (m *MockClient) ListOnDemandPrices(ctx context.Context, region string) ([]string, error) {
	args := m.Called(ctx, region)
	return args.Get(0).([]string), args.Error(1)
}

func (m *MockClient) ListStoragePrices(ctx context.Context, region string) ([]string, error) {
	args := m.Called(ctx, region)
	return args.Get(0).([]string), args.Error(1)
}

func (m *MockClient) ListEC2ServicePrices(ctx context.Context, region string, filters []pricingTypes.Filter) ([]string, error) {
	args := m.Called(ctx, region, filters)
	return args.Get(0).([]string), args.Error(1)
}

func (m *MockClient) ListVPCServicePrices(ctx context.Context, region string, filters []pricingTypes.Filter) ([]string, error) {
	args := m.Called(ctx, region, filters)
	return args.Get(0).([]string), args.Error(1)
}

func (m *MockClient) ListELBPrices(ctx context.Context, region string) ([]string, error) {
	args := m.Called(ctx, region)
	return args.Get(0).([]string), args.Error(1)
}

func (m *MockClient) DescribeLoadBalancers(ctx context.Context) ([]elbv2Types.LoadBalancer, error) {
	args := m.Called(ctx)
	return args.Get(0).([]elbv2Types.LoadBalancer), args.Error(1)
}

func (m *MockClient) ListRDSInstances(ctx context.Context) ([]rdsTypes.DBInstance, error) {
	args := m.Called(ctx)
	return args.Get(0).([]rdsTypes.DBInstance), args.Error(1)
}

func (m *MockClient) GetRDSUnitData(ctx context.Context, instType, region, deploymentOption, engineCode, isOutpost string) (string, error) {
	args := m.Called(ctx, instType, region, deploymentOption, engineCode, isOutpost)
	return args.Get(0).(string), args.Error(1)
}

func (m *MockClient) Metrics() []prometheus.Collector {
	args := m.Called()
	return args.Get(0).([]prometheus.Collector)
}

func stringPtr(s string) *string {
	return &s
}

func TestNew(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	mockClient := &MockClient{}

	// Set up mock expectations for VPC pricing calls
	mockClient.On("ListVPCServicePrices", mock.Anything, mock.AnythingOfType("string"), mock.Anything).Return([]string{
		`{"product":{"attributes":{"usagetype":"USE1-VpcEndpoint-Hours","regionCode":"us-east-1"}},"terms":{"OnDemand":{"test":{"priceDimensions":{"test":{"pricePerUnit":{"USD":"0.01"}}}}}}}`,
		`{"product":{"attributes":{"usagetype":"USE1-TransitGateway-Hours","regionCode":"us-east-1"}},"terms":{"OnDemand":{"test":{"priceDimensions":{"test":{"pricePerUnit":{"USD":"0.05"}}}}}}}`,
		`{"product":{"attributes":{"usagetype":"USE1-PublicIPv4:InUseAddress","regionCode":"us-east-1"}},"terms":{"OnDemand":{"test":{"priceDimensions":{"test":{"pricePerUnit":{"USD":"0.005"}}}}}}}`,
	}, nil)

	regions := []ec2Types.Region{
		{RegionName: stringPtr("us-east-1")},
		{RegionName: stringPtr("us-west-2")},
	}

	collector := New(t.Context(), &Config{
		ScrapeInterval: 1 * time.Hour,
		Regions:        regions,
		Logger:         logger,
		Client:         mockClient, // Add the dedicated client
	})

	assert.NotNil(t, collector)
	assert.NotNil(t, collector.pricingMap)
}

func TestCollectorName(t *testing.T) {
	collector := &Collector{}
	assert.Equal(t, "VPC", collector.Name())
}

func TestDescribe(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	collector := &Collector{logger: logger}

	ch := make(chan *prometheus.Desc, 10)

	err := collector.Describe(ch)
	assert.NoError(t, err)
}

// Test that VPC pricing map methods return errors when no data is available
func TestVPCPricingMapErrors(t *testing.T) {
	// Use a logger that discards output to reduce test noise
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	pricingMap := NewVPCPricingMap(logger)

	// Test error values when no pricing data is available
	_, err := pricingMap.GetVPCEndpointHourlyRate("us-east-1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get standard VPC endpoint pricing for region us-east-1")

	_, err = pricingMap.GetVPCServiceEndpointHourlyRate("us-east-1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get VPC service endpoint pricing for region us-east-1")

	_, err = pricingMap.GetTransitGatewayHourlyRate("us-east-1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get Transit Gateway pricing for region us-east-1")

	_, err = pricingMap.GetElasticIPInUseRate("us-east-1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get Elastic IP in-use pricing for region us-east-1")

	_, err = pricingMap.GetElasticIPIdleRate("us-east-1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get Elastic IP idle pricing for region us-east-1")
}
