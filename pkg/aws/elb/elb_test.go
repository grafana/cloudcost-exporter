package elb

import (
	"context"
	"log/slog"
	"testing"
	"time"

	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	elbTypes "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	"github.com/aws/aws-sdk-go-v2/service/pricing"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	elbv2client "github.com/grafana/cloudcost-exporter/pkg/aws/services/elbv2"
)

// MockELBv2Client is a mock implementation of the ELBv2 interface
type MockELBv2Client struct {
	mock.Mock
}

func (m *MockELBv2Client) DescribeLoadBalancers(ctx context.Context, params *elasticloadbalancingv2.DescribeLoadBalancersInput, optFns ...func(*elasticloadbalancingv2.Options)) (*elasticloadbalancingv2.DescribeLoadBalancersOutput, error) {
	args := m.Called(ctx, params)
	return args.Get(0).(*elasticloadbalancingv2.DescribeLoadBalancersOutput), args.Error(1)
}

func (m *MockELBv2Client) DescribeTargetGroups(ctx context.Context, params *elasticloadbalancingv2.DescribeTargetGroupsInput, optFns ...func(*elasticloadbalancingv2.Options)) (*elasticloadbalancingv2.DescribeTargetGroupsOutput, error) {
	args := m.Called(ctx, params)
	return args.Get(0).(*elasticloadbalancingv2.DescribeTargetGroupsOutput), args.Error(1)
}

// MockPricingClient is a mock implementation of the Pricing interface
type MockPricingClient struct {
	mock.Mock
}

func (m *MockPricingClient) GetProducts(ctx context.Context, input *pricing.GetProductsInput, optFns ...func(*pricing.Options)) (*pricing.GetProductsOutput, error) {
	args := m.Called(ctx, input)
	return args.Get(0).(*pricing.GetProductsOutput), args.Error(1)
}

func TestNew(t *testing.T) {
	config := &Config{
		ScrapeInterval: time.Minute,
		Regions: []ec2Types.Region{
			{RegionName: stringPtr("us-east-1")},
		},
		RegionClients: map[string]elbv2client.ELBv2{
			"us-east-1": &MockELBv2Client{},
		},
		Logger: slog.Default(),
	}

	mockPricing := &MockPricingClient{}
	collector := New(config, mockPricing)

	assert.NotNil(t, collector)
	assert.Equal(t, config.ScrapeInterval, collector.ScrapeInterval)
	assert.Equal(t, config.Regions, collector.Regions)
	assert.Equal(t, mockPricing, collector.pricingService)
	assert.NotNil(t, collector.pricingMap)
}

func TestCollectorName(t *testing.T) {
	config := &Config{
		ScrapeInterval: time.Minute,
		Regions:        []ec2Types.Region{},
		RegionClients:  map[string]elbv2client.ELBv2{},
		Logger:         slog.Default(),
	}

	collector := New(config, &MockPricingClient{})
	assert.Equal(t, "ELB", collector.Name())
}

func TestCollectorDescribe(t *testing.T) {
	config := &Config{
		ScrapeInterval: time.Minute,
		Regions:        []ec2Types.Region{},
		RegionClients:  map[string]elbv2client.ELBv2{},
		Logger:         slog.Default(),
	}

	collector := New(config, &MockPricingClient{})
	ch := make(chan *prometheus.Desc, 1)

	err := collector.Describe(ch)
	assert.NoError(t, err)

	desc := <-ch
	assert.Contains(t, desc.String(), "cloudcost_aws_elb_loadbalancer_total_usd_per_hour")
}

// MockRegistry is a simple mock for the provider.Registry interface
type MockRegistry struct {
	mock.Mock
}

func (m *MockRegistry) Register(c prometheus.Collector) error {
	args := m.Called(c)
	return args.Error(0)
}

func (m *MockRegistry) MustRegister(cs ...prometheus.Collector) {
	m.Called(cs)
}

func (m *MockRegistry) Unregister(c prometheus.Collector) bool {
	args := m.Called(c)
	return args.Bool(0)
}

func (m *MockRegistry) Gather() ([]*dto.MetricFamily, error) {
	args := m.Called()
	return args.Get(0).([]*dto.MetricFamily), args.Error(1)
}

func (m *MockRegistry) Describe(ch chan<- *prometheus.Desc) {
	m.Called(ch)
}

func (m *MockRegistry) Collect(ch chan<- prometheus.Metric) {
	m.Called(ch)
}

func TestCollectorRegister(t *testing.T) {
	config := &Config{
		ScrapeInterval: time.Minute,
		Regions:        []ec2Types.Region{},
		RegionClients:  map[string]elbv2client.ELBv2{},
		Logger:         slog.Default(),
	}

	collector := New(config, &MockPricingClient{})
	registry := &MockRegistry{}
	registry.On("Register", mock.Anything).Return(nil)

	err := collector.Register(registry)
	assert.NoError(t, err)
}

func TestCollectRegionLoadBalancers(t *testing.T) {
	config := &Config{
		ScrapeInterval: time.Minute,
		Regions:        []ec2Types.Region{},
		RegionClients:  map[string]elbv2client.ELBv2{},
		Logger:         slog.Default(),
	}

	collector := New(config, &MockPricingClient{})

	// Set up mock pricing data
	collector.pricingMap.SetRegionPricing("us-east-1", &RegionPricing{
		ALBHourlyRate: map[string]float64{"default": 0.0225},
		NLBHourlyRate: map[string]float64{"default": 0.0225},
		CLBHourlyRate: map[string]float64{"default": 0.025},
	})

	mockClient := &MockELBv2Client{}
	mockClient.On("DescribeLoadBalancers", mock.Anything, (*elasticloadbalancingv2.DescribeLoadBalancersInput)(nil)).Return(
		&elasticloadbalancingv2.DescribeLoadBalancersOutput{
			LoadBalancers: []elbTypes.LoadBalancer{
				{
					LoadBalancerName: stringPtr("test-alb"),
					LoadBalancerArn:  stringPtr("arn:aws:elasticloadbalancing:us-east-1:123456789012:loadbalancer/app/test-alb/1234567890123456"),
					Type:             elbTypes.LoadBalancerTypeEnumApplication,
					Scheme:           elbTypes.LoadBalancerSchemeEnumInternetFacing,
				},
				{
					LoadBalancerName: stringPtr("test-nlb"),
					LoadBalancerArn:  stringPtr("arn:aws:elasticloadbalancing:us-east-1:123456789012:loadbalancer/net/test-nlb/1234567890123456"),
					Type:             elbTypes.LoadBalancerTypeEnumNetwork,
					Scheme:           elbTypes.LoadBalancerSchemeEnumInternal,
				},
			},
		}, nil)

	loadBalancers, err := collector.collectRegionLoadBalancers("us-east-1", mockClient)

	assert.NoError(t, err)
	assert.Len(t, loadBalancers, 2)

	// Check ALB
	assert.Equal(t, "test-alb", loadBalancers[0].Name)
	assert.Equal(t, elbTypes.LoadBalancerTypeEnumApplication, loadBalancers[0].Type)
	assert.Equal(t, elbTypes.LoadBalancerSchemeEnumInternetFacing, loadBalancers[0].Scheme)
	assert.Equal(t, 0.0225, loadBalancers[0].Cost)

	// Check NLB
	assert.Equal(t, "test-nlb", loadBalancers[1].Name)
	assert.Equal(t, elbTypes.LoadBalancerTypeEnumNetwork, loadBalancers[1].Type)
	assert.Equal(t, elbTypes.LoadBalancerSchemeEnumInternal, loadBalancers[1].Scheme)
	assert.Equal(t, 0.0225, loadBalancers[1].Cost)

	mockClient.AssertExpectations(t)
}

func stringPtr(s string) *string {
	return &s
}
