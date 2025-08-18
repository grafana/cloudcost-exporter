package elb

import (
	"log/slog"
	"testing"
	"time"

	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	elbTypes "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/mock/gomock"

	"github.com/grafana/cloudcost-exporter/pkg/aws/client"
	mock_client "github.com/grafana/cloudcost-exporter/pkg/aws/client/mocks"
)

func TestNew(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockClient := mock_client.NewMockClient(ctrl)

	config := &Config{
		ScrapeInterval: time.Minute,
		Regions: []ec2Types.Region{
			{RegionName: stringPtr("us-east-1")},
		},
		RegionClients: map[string]client.Client{
			"us-east-1": mockClient,
		},
		Logger: slog.Default(),
	}

	collector := New(config)

	assert.NotNil(t, collector)
	assert.Equal(t, config.ScrapeInterval, collector.ScrapeInterval)
	assert.Equal(t, config.Regions, collector.Regions)
	assert.Equal(t, mockClient, collector.awsRegionClientMap["us-east-1"])
	assert.NotNil(t, collector.pricingMap)
}

func TestCollectorName(t *testing.T) {
	config := &Config{
		ScrapeInterval: time.Minute,
		Regions:        []ec2Types.Region{},
		RegionClients:  map[string]client.Client{},
		Logger:         slog.Default(),
	}

	collector := New(config)
	assert.Equal(t, subsystem, collector.Name())
}

func TestCollectorDescribe(t *testing.T) {
	config := &Config{
		ScrapeInterval: time.Minute,
		Regions:        []ec2Types.Region{},
		RegionClients:  map[string]client.Client{},
		Logger:         slog.Default(),
	}

	collector := New(config)
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
		RegionClients:  map[string]client.Client{},
		Logger:         slog.Default(),
	}

	collector := New(config)
	registry := &MockRegistry{}
	registry.On("Register", mock.Anything).Return(nil)

	err := collector.Register(registry)
	assert.NoError(t, err)
}

func TestCollectRegionLoadBalancers(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockClient := mock_client.NewMockClient(ctrl)
	mockClient.EXPECT().DescribeLoadBalancers(gomock.Any()).Return([]elbTypes.LoadBalancer{
		{
			LoadBalancerName: stringPtr("test-alb"),
			Type:             elbTypes.LoadBalancerTypeEnumApplication,
		},
		{
			LoadBalancerName: stringPtr("test-nlb"),
			Type:             elbTypes.LoadBalancerTypeEnumNetwork,
		},
	}, nil)

	config := &Config{
		ScrapeInterval: time.Minute,
		Regions:        []ec2Types.Region{},
		RegionClients: map[string]client.Client{
			"us-east-1": mockClient,
		},
		Logger: slog.Default(),
	}

	collector := New(config)

	// Set up mock pricing data
	collector.pricingMap.SetRegionPricing("us-east-1", &RegionPricing{
		ALBHourlyRate: map[string]float64{"default": 0.0225},
		NLBHourlyRate: map[string]float64{"default": 0.0225},
	})

	loadBalancers, err := collector.collectRegionLoadBalancers("us-east-1")

	assert.NoError(t, err)
	assert.Len(t, loadBalancers, 2)

	// Check ALB
	assert.Equal(t, "test-alb", loadBalancers[0].Name)
	assert.Equal(t, elbTypes.LoadBalancerTypeEnumApplication, loadBalancers[0].Type)
	assert.Equal(t, 0.0225, loadBalancers[0].Cost)

	// Check NLB
	assert.Equal(t, "test-nlb", loadBalancers[1].Name)
	assert.Equal(t, elbTypes.LoadBalancerTypeEnumNetwork, loadBalancers[1].Type)
	assert.Equal(t, 0.0225, loadBalancers[1].Cost)

}

func stringPtr(s string) *string {
	return &s
}
