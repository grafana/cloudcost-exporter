package natgateway_test

import (
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
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
			collector := natgateway.New(t.Context(), &natgateway.Config{
				ScrapeInterval: tt.ScrapeInterval,
				Regions:        []ec2Types.Region{{RegionName: aws.String(tt.regionName)}},
				Logger:         tt.Logger,
				RegionMap: map[string]awsclient.Client{
					tt.regionName: tt.regionClient,
				},
			})
			assert.NotNil(t, collector)
			assert.NotNil(t, collector.PricingStore)
			assert.Equal(t, tt.ScrapeInterval, utils.DefaultScrapeInterval)
		})
	}
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
			expectedDescCount: 2, // HourlyGaugeDesc and DataProcessingGaugeDesc
			expectedDescs: []string{
				natgateway.HourlyGaugeDesc.String(),
				natgateway.DataProcessingGaugeDesc.String(),
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
				prometheus.MustNewConstMetric(natgateway.HourlyGaugeDesc, prometheus.GaugeValue, 0.045, "us-east-1"),
				prometheus.MustNewConstMetric(natgateway.DataProcessingGaugeDesc, prometheus.GaugeValue, 0.045, "us-east-1"),
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			region := "us-east-1"
			collector := natgateway.New(t.Context(), &natgateway.Config{
				ScrapeInterval: 1 * time.Hour,
				Regions:        []ec2Types.Region{{RegionName: aws.String(region)}},
				Logger:         testLogger,
				RegionMap: map[string]awsclient.Client{
					region: tt.regionClient,
				},
			})

			ch := make(chan prometheus.Metric, len(tt.expectedMetrics))
			err := collector.Collect(t.Context(), ch)
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
