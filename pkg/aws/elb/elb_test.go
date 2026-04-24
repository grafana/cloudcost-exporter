package elb

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	elbTypes "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/grafana/cloudcost-exporter/pkg/aws/client"
	mock_client "github.com/grafana/cloudcost-exporter/pkg/aws/client/mocks"
	"github.com/grafana/cloudcost-exporter/pkg/utils"
)

func TestNew(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockClient := mock_client.NewMockClient(ctrl)

	config := &Config{
		ScrapeInterval: time.Minute,
		Regions: []ec2Types.Region{
			{RegionName: utils.StringPtr("us-east-1")},
		},
		PricingClient: mockClient,
		RegionMap: map[string]client.Client{
			"us-east-1": mockClient,
		},
		AccountID: "123456789012",
	}

	collector, err := New(context.Background(), config, slog.Default())
	require.NoError(t, err)

	assert.NotNil(t, collector)
	assert.Equal(t, config.ScrapeInterval, collector.ScrapeInterval)
	assert.Equal(t, config.Regions, collector.regions)
	assert.Equal(t, mockClient, collector.pricingClient)
	assert.Equal(t, mockClient, collector.awsRegionClientMap["us-east-1"])
	assert.NotNil(t, collector.pricingMap)
}

func TestCollectorName(t *testing.T) {
	config := &Config{
		ScrapeInterval: time.Minute,
		Regions:        []ec2Types.Region{},
		RegionMap:      map[string]client.Client{},
	}

	collector, err := New(context.Background(), config, slog.Default())
	require.NoError(t, err)
	assert.Equal(t, subsystem, collector.Name())
}

func TestCollectorDescribe(t *testing.T) {
	config := &Config{
		ScrapeInterval: time.Minute,
		Regions:        []ec2Types.Region{},
		RegionMap:      map[string]client.Client{},
	}
	expectedDescs := []string{
		LoadBalancerUsageHourlyCostDesc.String(),
		LoadBalancerCapacityUnitsUsageHourlyCostDesc.String(),
	}
	collector, err := New(context.Background(), config, slog.Default())
	require.NoError(t, err)
	ch := make(chan *prometheus.Desc, len(expectedDescs))

	err = collector.Describe(ch)
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
			LoadBalancerName: utils.StringPtr("test-alb"),
			Type:             elbTypes.LoadBalancerTypeEnumApplication,
		},
		{
			LoadBalancerName: utils.StringPtr("test-nlb"),
			Type:             elbTypes.LoadBalancerTypeEnumNetwork,
		},
	}, nil)

	config := &Config{
		ScrapeInterval: time.Minute,
		Regions:        []ec2Types.Region{},
		RegionMap: map[string]client.Client{
			"us-east-1": mockClient,
		},
		AccountID: "123456789012",
	}

	collector, err := New(context.Background(), config, slog.Default())
	require.NoError(t, err)

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

func TestFetchRegionPricing(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockClient := mock_client.NewMockClient(ctrl)

	albProduct := `{"Product":{"Attributes":{"usageType":"USE1-LoadBalancerUsage","operation":"LoadBalancing:Application"}},"Terms":{"OnDemand":{"t1":{"PriceDimensions":{"d1":{"pricePerUnit":{"USD":"0.0225"}}}}}}}`
	nlbProduct := `{"Product":{"Attributes":{"usageType":"USE1-LCUUsage","operation":"LoadBalancing:Network"}},"Terms":{"OnDemand":{"t1":{"PriceDimensions":{"d1":{"pricePerUnit":{"USD":"0.006"}}}}}}}`
	mockClient.EXPECT().ListELBPrices(gomock.Any(), "us-east-1").Return([]string{albProduct, nlbProduct}, nil)

	pm := NewELBPricingMap(slog.Default())
	pricing, err := pm.FetchRegionPricing(mockClient, t.Context(), "us-east-1")

	assert.NoError(t, err)
	assert.Equal(t, 0.0225, pricing.ALBHourlyRate[LoadBalancerUsage])
	assert.Equal(t, 0.006, pricing.NLBHourlyRate[LCUUsage])
}

func TestCollect_EmitsMetrics(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockPricingClient := mock_client.NewMockClient(ctrl)
	mockRegionClient := mock_client.NewMockClient(ctrl)

	albLBUsage := `{"Product":{"Attributes":{"usageType":"USE1-LoadBalancerUsage","operation":"LoadBalancing:Application"}},"Terms":{"OnDemand":{"t1":{"PriceDimensions":{"d1":{"pricePerUnit":{"USD":"0.0225"}}}}}}}`
	albLCUUsage := `{"Product":{"Attributes":{"usageType":"USE1-LCUUsage","operation":"LoadBalancing:Application"}},"Terms":{"OnDemand":{"t1":{"PriceDimensions":{"d1":{"pricePerUnit":{"USD":"0.008"}}}}}}}`
	mockPricingClient.EXPECT().ListELBPrices(gomock.Any(), "us-east-1").Return([]string{albLBUsage, albLCUUsage}, nil)
	mockRegionClient.EXPECT().DescribeLoadBalancers(gomock.Any()).Return([]elbTypes.LoadBalancer{
		{
			LoadBalancerName: utils.StringPtr("test-alb"),
			Type:             elbTypes.LoadBalancerTypeEnumApplication,
		},
	}, nil)

	config := &Config{
		ScrapeInterval: time.Minute,
		Regions:        []ec2Types.Region{{RegionName: utils.StringPtr("us-east-1")}},
		PricingClient:  mockPricingClient,
		RegionMap:      map[string]client.Client{"us-east-1": mockRegionClient},
		AccountID:      "123456789012",
	}

	collector, err := New(context.Background(), config, slog.Default())
	require.NoError(t, err)

	ch := make(chan prometheus.Metric, 10)
	err = collector.Collect(t.Context(), ch)
	close(ch)

	require.NoError(t, err)

	var metrics []prometheus.Metric
	for m := range ch {
		metrics = append(metrics, m)
	}
	require.Len(t, metrics, 2)

	descStrings := make(map[string]struct{}, 2)
	for _, m := range metrics {
		descStrings[m.Desc().String()] = struct{}{}
	}
	assert.Contains(t, descStrings, LoadBalancerUsageHourlyCostDesc.String())
	assert.Contains(t, descStrings, LoadBalancerCapacityUnitsUsageHourlyCostDesc.String())
}

func TestFetchRegionPricingError(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockClient := mock_client.NewMockClient(ctrl)
	mockClient.EXPECT().ListELBPrices(gomock.Any(), "us-east-1").Return(nil, errors.New("api error"))

	pm := NewELBPricingMap(slog.Default())
	pricing, err := pm.FetchRegionPricing(mockClient, t.Context(), "us-east-1")

	assert.Error(t, err)
	assert.Nil(t, pricing)
}

func TestRefresh(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockClient := mock_client.NewMockClient(ctrl)

	albProduct := `{"Product":{"Attributes":{"usageType":"USE1-LoadBalancerUsage","operation":"LoadBalancing:Application"}},"Terms":{"OnDemand":{"t1":{"PriceDimensions":{"d1":{"pricePerUnit":{"USD":"0.0225"}}}}}}}`
	mockClient.EXPECT().ListELBPrices(gomock.Any(), "us-east-1").Return([]string{albProduct}, nil)
	mockClient.EXPECT().ListELBPrices(gomock.Any(), "us-west-2").Return([]string{albProduct}, nil)

	pm := NewELBPricingMap(slog.Default())
	regions := []ec2Types.Region{
		{RegionName: utils.StringPtr("us-east-1")},
		{RegionName: utils.StringPtr("us-west-2")},
	}

	err := pm.refresh(t.Context(), mockClient, regions)
	assert.NoError(t, err)

	for _, region := range []string{"us-east-1", "us-west-2"} {
		pricing, err := pm.GetRegionPricing(region)
		assert.NoError(t, err)
		assert.Equal(t, 0.0225, pricing.ALBHourlyRate[LoadBalancerUsage])
	}
}
