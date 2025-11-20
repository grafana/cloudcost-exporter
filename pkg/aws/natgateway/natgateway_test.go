package natgateway_test

import (
	"log/slog"
	"os"
	"strings"
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

func TestCollector_CollectAggregatesMultipleUsageTypesPerRegion(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	region := "us-east-1"

	// Mock client returns multiple usage types for the same region that should be aggregated
	regionClient := func() *mock_client.MockClient {
		m := mock_client.NewMockClient(ctrl)
		m.EXPECT().
			ListEC2ServicePrices(gomock.Any(), region, testNATGatewayFilters).
			Return([]string{
				// Two hourly usage types that both map to NATGatewayHours
				`{"product":{"attributes":{"usagetype":"USE1-NatGateway-Hours","regionCode":"us-east-1"}},"terms":{"OnDemand":{"test1":{"priceDimensions":{"dim1":{"pricePerUnit":{"USD":"0.040"}}}}}}}`,
				`{"product":{"attributes":{"usagetype":"USE1-NatGateway-Hours-Additional","regionCode":"us-east-1"}},"terms":{"OnDemand":{"test2":{"priceDimensions":{"dim2":{"pricePerUnit":{"USD":"0.010"}}}}}}}`,
				// Two data processing usage types that both map to NATGatewayBytes
				`{"product":{"attributes":{"usagetype":"USE1-NatGateway-Bytes","regionCode":"us-east-1"}},"terms":{"OnDemand":{"test3":{"priceDimensions":{"dim3":{"pricePerUnit":{"USD":"0.050"}}}}}}}`,
				`{"product":{"attributes":{"usagetype":"USE1-NatGateway-Bytes-Additional","regionCode":"us-east-1"}},"terms":{"OnDemand":{"test4":{"priceDimensions":{"dim4":{"pricePerUnit":{"USD":"0.005"}}}}}}}`,
			}, nil).
			Times(1)
		return m
	}()

	collector := natgateway.New(t.Context(), &natgateway.Config{
		ScrapeInterval: 1 * time.Hour,
		Regions:        []ec2Types.Region{{RegionName: aws.String(region)}},
		Logger:         testLogger,
		RegionMap: map[string]awsclient.Client{
			region: regionClient,
		},
	})

	ch := make(chan prometheus.Metric, 10)
	err := collector.Collect(t.Context(), ch)
	close(ch)

	assert.NoError(t, err)

	var results []*utils.MetricResult
	for metric := range ch {
		mr := utils.ReadMetrics(metric)
		if mr != nil {
			results = append(results, mr)
		}
	}

	// We expect exactly one hourly and one data-processing metric for the region
	assert.Len(t, results, 2)

	var (
		hourlyMetric         *utils.MetricResult
		dataProcessingMetric *utils.MetricResult
	)

	for _, mr := range results {
		if strings.Contains(mr.FqName, "hourly_rate_usd_per_hour") {
			hourlyMetric = mr
		} else if strings.Contains(mr.FqName, "data_processing_usd_per_gb") {
			dataProcessingMetric = mr
		}
	}

	if assert.NotNil(t, hourlyMetric, "expected hourly metric to be present") {
		assert.Equal(t, region, hourlyMetric.Labels["region"])
		// 0.040 + 0.010 = 0.050
		assert.InDelta(t, 0.050, hourlyMetric.Value, 1e-9)
	}

	if assert.NotNil(t, dataProcessingMetric, "expected data processing metric to be present") {
		assert.Equal(t, region, dataProcessingMetric.Labels["region"])
		// 0.050 + 0.005 = 0.055
		assert.InDelta(t, 0.055, dataProcessingMetric.Value, 1e-9)
	}
}
