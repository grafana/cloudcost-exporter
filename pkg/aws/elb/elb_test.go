package elb

import (
	"log/slog"
	"testing"
	"time"

	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	elbTypes "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
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
	expectedDescs := []string{
		LoadBalancerUsageHourlyCostDesc.String(),
		LoadBalancerCapacityUnitsUsageHourlyCostDesc.String(),
	}
	collector := New(config)
	ch := make(chan *prometheus.Desc, len(expectedDescs))

	err := collector.Describe(ch)
	close(ch)

	assert.NoError(t, err)

	var descs []string
	for desc := range ch {
		assert.NotNil(t, desc)
		descs = append(descs, desc.String())
	}
	assert.Equal(t, expectedDescs, descs)
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
		ALBHourlyRate: map[string]float64{LCUUsage: 0.008, LoadBalancerUsage: 0.0225},
		NLBHourlyRate: map[string]float64{LCUUsage: 0.008, LoadBalancerUsage: 0.0225},
	})

	loadBalancers, err := collector.collectRegionLoadBalancers(t.Context(), "us-east-1")

	assert.NoError(t, err)
	assert.Len(t, loadBalancers, 2)

	// Check ALB
	assert.Equal(t, "test-alb", loadBalancers[0].Name)
	assert.Equal(t, elbTypes.LoadBalancerTypeEnumApplication, loadBalancers[0].Type)
	assert.Equal(t, 0.008, loadBalancers[0].LCUUsageCost)
	assert.Equal(t, 0.0225, loadBalancers[0].LoadBalancerUsageCost)

	// Check NLB
	assert.Equal(t, "test-nlb", loadBalancers[1].Name)
	assert.Equal(t, elbTypes.LoadBalancerTypeEnumNetwork, loadBalancers[1].Type)
	assert.Equal(t, 0.008, loadBalancers[1].LCUUsageCost)
	assert.Equal(t, 0.0225, loadBalancers[1].LoadBalancerUsageCost)

}

func stringPtr(s string) *string {
	return &s
}
