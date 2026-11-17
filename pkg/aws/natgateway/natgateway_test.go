package natgateway_test

import (
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	aws "github.com/aws/aws-sdk-go-v2/aws"
	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	pricingTypes "github.com/aws/aws-sdk-go-v2/service/pricing/types"

	awsclient "github.com/grafana/cloudcost-exporter/pkg/aws/client"
	mock_client "github.com/grafana/cloudcost-exporter/pkg/aws/client/mocks"
	"github.com/grafana/cloudcost-exporter/pkg/aws/natgateway"
	"github.com/grafana/cloudcost-exporter/pkg/utils"
)

var (
	testLogger            = slog.New(slog.NewTextHandler(os.Stdout, nil))
	testNATGatewayFilters = []pricingTypes.Filter{
		{
			Field: aws.String("productFamily"),
			Type:  pricingTypes.FilterTypeTermMatch,
			Value: aws.String("NAT Gateway"),
		},
	}
)

func TestNew(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	tests := map[string]struct {
		ScrapeInterval time.Duration
		Regions        []ec2Types.Region
		Logger         *slog.Logger
		regionName     string
		regionClient   *mock_client.MockClient
	}{
		"creates new collector with a valid config": {
			ScrapeInterval: 1 * time.Hour,
			regionName:     "us-east-1",
			Logger:         testLogger,
			regionClient: func() *mock_client.MockClient {
				m := mock_client.NewMockClient(ctrl)
				m.EXPECT().
					ListEC2ServicePrices(gomock.Any(), "us-east-1", testNATGatewayFilters).
					Return([]string{
						`{"product":{"attributes":{"usagetype":"USE1-NatGateway-Hours","regionCode":"us-east-1"}},"terms":{"OnDemand":{"test":{"priceDimensions":{"test":{"pricePerUnit":{"USD":"0.045"}}}}}}}`,
					}, nil).
					Times(1)
				return m
			}(),
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			collector, err := natgateway.New(t.Context(), &natgateway.Config{
				ScrapeInterval: tt.ScrapeInterval,
				Regions:        []ec2Types.Region{{RegionName: aws.String(tt.regionName)}},
				RegionMap: map[string]awsclient.Client{
					tt.regionName: tt.regionClient,
				},
				AccountID: "123456789012",
			}, tt.Logger)
			require.NoError(t, err)
			assert.NotNil(t, collector)
			assert.NotNil(t, collector.PricingStore)
			assert.Equal(t, tt.ScrapeInterval, utils.DefaultScrapeInterval)
		})
	}
}

func TestNew_ReturnsErrorWhenPricingAPIUnavailable(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	regionClient := mock_client.NewMockClient(ctrl)
	regionClient.EXPECT().
		ListEC2ServicePrices(gomock.Any(), "us-east-1", gomock.Any()).
		Return(nil, fmt.Errorf("pricing API unavailable")).
		Times(1)

	collector, err := natgateway.New(t.Context(), &natgateway.Config{
		Regions: []ec2Types.Region{{RegionName: aws.String("us-east-1")}},
		RegionMap: map[string]awsclient.Client{
			"us-east-1": regionClient,
		},
	}, testLogger)

	assert.Nil(t, collector)
	assert.Error(t, err)
}

func TestCollector_Name(t *testing.T) {
	tests := map[string]struct {
		expectedName string
	}{
		"returns correct name": {
			expectedName: "NATGATEWAY",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			c := &natgateway.Collector{}
			assert.Equal(t, tt.expectedName, c.Name())
		})
	}
}

func TestCollector_Describe(t *testing.T) {
	tests := map[string]struct {
		expectedDescCount int
		expectedDescs     []string
	}{
		"expect correct descriptions": {
			expectedDescCount: 4,
			expectedDescs: []string{
				natgateway.HourlyGaugeDesc.String(),
				natgateway.DataProcessingGaugeDesc.String(),
				natgateway.RegionalHourlyGaugeDesc.String(),
				natgateway.RegionalDataProcessingGaugeDesc.String(),
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			c := &natgateway.Collector{}
			ch := make(chan *prometheus.Desc, tt.expectedDescCount)

			err := c.Describe(ch)
			close(ch)

			assert.NoError(t, err)

			var descs []string
			var descCount int
			for desc := range ch {
				assert.NotNil(t, desc)
				descs = append(descs, desc.String())
				descCount++
			}
			assert.Equal(t, tt.expectedDescCount, descCount)
			assert.Equal(t, tt.expectedDescs, descs)
		})
	}
}

func TestCollector_Collect(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	tests := map[string]struct {
		expectedMetrics []prometheus.Metric
		regionClient    *mock_client.MockClient
	}{
		"validate metrics": {
			regionClient: func() *mock_client.MockClient {
				m := mock_client.NewMockClient(ctrl)
				m.EXPECT().
					ListEC2ServicePrices(gomock.Any(), "us-east-1", testNATGatewayFilters).
					Return([]string{
						`{"product":{"attributes":{"usagetype":"USE1-NatGateway-Hours","regionCode":"us-east-1"}},"terms":{"OnDemand":{"test":{"priceDimensions":{"test":{"pricePerUnit":{"USD":"0.045"}}}}}}}`,
						`{"product":{"attributes":{"usagetype":"USE1-NatGateway-Bytes","regionCode":"us-east-1"}},"terms":{"OnDemand":{"test":{"priceDimensions":{"test":{"pricePerUnit":{"USD":"0.045"}}}}}}}`,
					}, nil).
					Times(1)
				return m
			}(),
			expectedMetrics: []prometheus.Metric{
				prometheus.MustNewConstMetric(natgateway.HourlyGaugeDesc, prometheus.GaugeValue, 0.045, "123456789012", "us-east-1"),
				prometheus.MustNewConstMetric(natgateway.DataProcessingGaugeDesc, prometheus.GaugeValue, 0.045, "123456789012", "us-east-1"),
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			region := "us-east-1"
			collector, err := natgateway.New(t.Context(), &natgateway.Config{
				ScrapeInterval: 1 * time.Hour,
				Regions:        []ec2Types.Region{{RegionName: aws.String(region)}},
				RegionMap: map[string]awsclient.Client{
					region: tt.regionClient,
				},
				AccountID: "123456789012",
			}, testLogger)
			require.NoError(t, err)

			ch := make(chan prometheus.Metric, len(tt.expectedMetrics))
			err = collector.Collect(t.Context(), ch)
			close(ch)

			assert.NoError(t, err)

			var metrics []prometheus.Metric
			for metric := range ch {
				assert.Contains(t, tt.expectedMetrics, metric)
				metrics = append(metrics, metric)
			}
			assert.Len(t, metrics, len(tt.expectedMetrics))
		})
	}
}

func TestCollector_CollectSeparatesGatewayTypes(t *testing.T) {
	const accountID = "123456789012"
	tests := []struct {
		name     string
		region   string
		prices   map[string]string
		expected map[string]float64
	}{
		{
			name:   "standard and regional prices with unprefixed aliases",
			region: "us-east-1",
			prices: map[string]string{
				"NatGateway-Hours":              "0.045",
				"NatGateway-Bytes":              "0.045",
				"RegionalNatGateway-Hours":      "0.050",
				"USE1-RegionalNatGateway-Hours": "0.050",
				"USE1-RegionalNatGateway-Bytes": "0.060",
				"NatGateway-Hours-Additional":   "0.010",
				"NatGateway-Bytes-Additional":   "0.010",
			},
			expected: map[string]float64{
				"cloudcost_aws_natgateway_hourly_rate_usd_per_hour":            0.045,
				"cloudcost_aws_natgateway_data_processing_usd_per_gb":          0.045,
				"cloudcost_aws_natgateway_regional_hourly_rate_usd_per_hour":   0.050,
				"cloudcost_aws_natgateway_regional_data_processing_usd_per_gb": 0.060,
			},
		},
		{
			name:   "region-prefixed standard and regional prices",
			region: "mx-central-1",
			prices: map[string]string{
				"MXC1-NatGateway-Hours":         "0.04725",
				"MXC1-NatGateway-Bytes":         "0.04725",
				"MXC1-RegionalNatGateway-Hours": "0.04725",
				"MXC1-RegionalNatGateway-Bytes": "0.04725",
			},
			expected: map[string]float64{
				"cloudcost_aws_natgateway_hourly_rate_usd_per_hour":            0.04725,
				"cloudcost_aws_natgateway_data_processing_usd_per_gb":          0.04725,
				"cloudcost_aws_natgateway_regional_hourly_rate_usd_per_hour":   0.04725,
				"cloudcost_aws_natgateway_regional_data_processing_usd_per_gb": 0.04725,
			},
		},
		{
			name:   "regional-only catalog does not emit standard prices",
			region: "us-east-1",
			prices: map[string]string{
				"RegionalNatGateway-Hours": "0.050",
				"RegionalNatGateway-Bytes": "0.060",
			},
			expected: map[string]float64{
				"cloudcost_aws_natgateway_regional_hourly_rate_usd_per_hour":   0.050,
				"cloudcost_aws_natgateway_regional_data_processing_usd_per_gb": 0.060,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			client := mock_client.NewMockClient(ctrl)
			var products []string
			for usageType, price := range tt.prices {
				products = append(products, fmt.Sprintf(`{"product":{"attributes":{"usagetype":%q,"regionCode":%q}},"terms":{"OnDemand":{"test":{"priceDimensions":{"test":{"pricePerUnit":{"USD":%q}}}}}}}`, usageType, tt.region, price))
			}
			client.EXPECT().ListEC2ServicePrices(gomock.Any(), tt.region, testNATGatewayFilters).Return(products, nil)
			collector, err := natgateway.New(t.Context(), &natgateway.Config{
				Regions:   []ec2Types.Region{{RegionName: aws.String(tt.region)}},
				RegionMap: map[string]awsclient.Client{tt.region: client},
				AccountID: accountID,
			}, testLogger)
			require.NoError(t, err)

			ch := make(chan prometheus.Metric, 10)
			require.NoError(t, collector.Collect(t.Context(), ch))
			close(ch)

			values := make(map[string]float64)
			for metric := range ch {
				result := utils.ReadMetrics(metric)
				require.NotNil(t, result)
				require.NotContains(t, values, result.FqName, "duplicate metric")
				assert.Equal(t, utils.LabelMap{"account_id": accountID, "region": tt.region}, result.Labels)
				values[result.FqName] = result.Value
			}
			assert.Equal(t, tt.expected, values)
		})
	}
}
