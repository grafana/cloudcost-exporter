package s3

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/grafana/cloudcost-exporter/pkg/aws/client"
	mock_client "github.com/grafana/cloudcost-exporter/pkg/aws/client/mocks"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	mock_provider "github.com/grafana/cloudcost-exporter/pkg/provider/mocks"
)

func TestNewCollector(t *testing.T) {
	type args struct {
		interval time.Duration
	}
	tests := map[string]struct {
		args args
		want *Collector
	}{
		"Create a new collector": {
			args: args{
				interval: time.Duration(1) * time.Hour,
			},
			want: &Collector{},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			c := mock_client.NewMockClient(ctrl)

			got := New(tt.args.interval, c)
			assert.NotNil(t, got)
			assert.Equal(t, tt.args.interval, got.interval)
		})
	}
}

func TestCollector_Name(t *testing.T) {
	c := &Collector{}
	require.Equal(t, "S3", c.Name())
}

func TestCollector_Register(t *testing.T) {
	ctrl := gomock.NewController(t)
	r := mock_provider.NewMockRegistry(ctrl)
	r.EXPECT().MustRegister(gomock.Any()).Times(4)

	client := mock_client.NewMockClient(ctrl)
	client.EXPECT().Metrics().Return([]prometheus.Collector{}).Times(1)

	c := &Collector{
		client: client,
	}
	err := c.Register(r)
	require.NoError(t, err)
}

func TestCollector_Collect(t *testing.T) {
	timeInPast := time.Now().Add(-48 * time.Hour)
	withoutNextScrape := []string{
		"cloudcost_aws_s3_storage_by_location_usd_per_gibyte_hour",
		"cloudcost_aws_s3_operation_by_location_usd_per_krequest",
	}

	for _, tc := range []struct {
		name           string
		nextScrape     time.Time
		GetBillingData func(ctx context.Context, startDate time.Time, endDate time.Time) (*client.BillingData, error)

		// metricNames can be nil to check all metrics, or a set of strings form an allow list of metrics to check.
		metricNames        []string
		expectedResponse   float64
		expectedExposition string
	}{
		{
			name:       "cost and usage error is bubbled-up",
			nextScrape: timeInPast,
			GetBillingData: func(ctx context.Context, startDate time.Time, endDate time.Time) (*client.BillingData, error) {
				return nil, fmt.Errorf("test cost and usage error")
			},
			expectedResponse: 0.0,
		},
		{
			name:             "cost and usage output - three results with keys and valid region without a hyphen",
			nextScrape:       timeInPast,
			expectedResponse: 1.0,
			GetBillingData: func(ctx context.Context, startDate time.Time, endDate time.Time) (*client.BillingData, error) {
				return &client.BillingData{Regions: map[string]*client.PricingModel{
					"ap-northeast-1": {
						Model: map[string]*client.Pricing{
							"Requests-Tier1": {},
						},
					},
					"ap-northeast-2": {
						Model: map[string]*client.Pricing{
							"Requests-Tier2": {},
						},
					},
					"ap-northeast-3": {
						Model: map[string]*client.Pricing{
							"TimedStorage": {},
						},
					},
				}}, nil
			},
			metricNames: withoutNextScrape,
			expectedExposition: `
# HELP cloudcost_aws_s3_operation_by_location_usd_per_krequest Operation cost of S3 objects by region, class, and tier. Cost represented in USD/(1k req)
# TYPE cloudcost_aws_s3_operation_by_location_usd_per_krequest gauge
cloudcost_aws_s3_operation_by_location_usd_per_krequest{class="StandardStorage",region="ap-northeast-1",tier="1"} 0
cloudcost_aws_s3_operation_by_location_usd_per_krequest{class="StandardStorage",region="ap-northeast-2",tier="2"} 0
# HELP cloudcost_aws_s3_storage_by_location_usd_per_gibyte_hour Storage cost of S3 objects by region, class, and tier. Cost represented in USD/(GiB*h)
# TYPE cloudcost_aws_s3_storage_by_location_usd_per_gibyte_hour gauge
cloudcost_aws_s3_storage_by_location_usd_per_gibyte_hour{class="StandardStorage",region="ap-northeast-3"} 0
`,
		},
		{
			name:             "cost and usage output - results with two pages",
			nextScrape:       timeInPast,
			expectedResponse: 1.0,
			GetBillingData: func(ctx context.Context, startDate time.Time, endDate time.Time) (*client.BillingData, error) {
				return &client.BillingData{Regions: map[string]*client.PricingModel{
					"ap-northeast-1": {
						Model: map[string]*client.Pricing{
							"Requests-Tier1": {},
						},
					},
					"ap-northeast-2": {
						Model: map[string]*client.Pricing{
							"Requests-Tier2": {},
						},
					},
				}}, nil
			},
			metricNames: []string{
				"cloudcost_aws_s3_operation_by_location_usd_per_krequest",
			},
			expectedExposition: `
# HELP cloudcost_aws_s3_operation_by_location_usd_per_krequest Operation cost of S3 objects by region, class, and tier. Cost represented in USD/(1k req)
# TYPE cloudcost_aws_s3_operation_by_location_usd_per_krequest gauge
cloudcost_aws_s3_operation_by_location_usd_per_krequest{class="StandardStorage",region="ap-northeast-1",tier="1"} 0
cloudcost_aws_s3_operation_by_location_usd_per_krequest{class="StandardStorage",region="ap-northeast-2",tier="2"} 0
`,
		},
		{
			name:             "cost and usage output - result with nil amount",
			nextScrape:       timeInPast,
			expectedResponse: 1.0,
			GetBillingData: func(ctx context.Context, startDate time.Time, endDate time.Time) (*client.BillingData, error) {
				return &client.BillingData{Regions: map[string]*client.PricingModel{
					"ap-northeast-1": {
						Model: map[string]*client.Pricing{
							"Requests-Tier1": {},
						},
					},
				}}, nil
			},
			metricNames: []string{
				"cloudcost_aws_s3_operation_by_location_usd_per_krequest",
			},
			expectedExposition: `
# HELP cloudcost_aws_s3_operation_by_location_usd_per_krequest Operation cost of S3 objects by region, class, and tier. Cost represented in USD/(1k req)
# TYPE cloudcost_aws_s3_operation_by_location_usd_per_krequest gauge
cloudcost_aws_s3_operation_by_location_usd_per_krequest{class="StandardStorage",region="ap-northeast-1",tier="1"} 0
`,
		},
		{
			name:             "cost and usage output - result with invalid amount",
			nextScrape:       timeInPast,
			expectedResponse: 1.0,
			GetBillingData: func(ctx context.Context, startDate time.Time, endDate time.Time) (*client.BillingData, error) {
				return &client.BillingData{Regions: map[string]*client.PricingModel{
					"ap-northeast-1": {
						Model: map[string]*client.Pricing{
							"Requests-Tier1": {
								Usage: -32,
								Cost:  -3,
							},
						},
					},
				}}, nil
			},
			metricNames: []string{
				"cloudcost_aws_s3_operation_by_location_usd_per_krequest",
			},
			expectedExposition: `
# HELP cloudcost_aws_s3_operation_by_location_usd_per_krequest Operation cost of S3 objects by region, class, and tier. Cost represented in USD/(1k req)
# TYPE cloudcost_aws_s3_operation_by_location_usd_per_krequest gauge
cloudcost_aws_s3_operation_by_location_usd_per_krequest{class="StandardStorage",region="ap-northeast-1",tier="1"} 0
`,
		},
		{
			name:             "cost and usage output - result with nil unit",
			nextScrape:       timeInPast,
			expectedResponse: 1.0,
			GetBillingData: func(ctx context.Context, startDate time.Time, endDate time.Time) (*client.BillingData, error) {
				return &client.BillingData{Regions: map[string]*client.PricingModel{
					"ap-northeast-1": {
						Model: map[string]*client.Pricing{
							"Requests-Tier1": {
								UnitCost: 1000,
							},
						},
					},
				}}, nil
			},
			metricNames: []string{
				"cloudcost_aws_s3_operation_by_location_usd_per_krequest",
			},
			expectedExposition: `
# HELP cloudcost_aws_s3_operation_by_location_usd_per_krequest Operation cost of S3 objects by region, class, and tier. Cost represented in USD/(1k req)
# TYPE cloudcost_aws_s3_operation_by_location_usd_per_krequest gauge
cloudcost_aws_s3_operation_by_location_usd_per_krequest{class="StandardStorage",region="ap-northeast-1",tier="1"} 1000
`,
		},
		{
			name:             "cost and usage output - result with valid amount and unit",
			nextScrape:       timeInPast,
			expectedResponse: 1.0,
			GetBillingData: func(ctx context.Context, startDate time.Time, endDate time.Time) (*client.BillingData, error) {
				return &client.BillingData{Regions: map[string]*client.PricingModel{
					"ap-northeast-1": {
						Model: map[string]*client.Pricing{
							"Requests-Tier1": {
								Units:    "unit",
								UnitCost: 1000,
							},
							"Requests-Tier2": {
								Units:    "unit",
								UnitCost: 1000,
							},
							"TimedStorage": {
								Cost:     1,
								Units:    "unit",
								UnitCost: 0.0013689253935660506,
							},
							"unknown": {
								Units:    "unit",
								UnitCost: 1,
							},
						},
					},
				}}, nil
			},
			metricNames: withoutNextScrape,
			expectedExposition: `
# HELP cloudcost_aws_s3_operation_by_location_usd_per_krequest Operation cost of S3 objects by region, class, and tier. Cost represented in USD/(1k req)
# TYPE cloudcost_aws_s3_operation_by_location_usd_per_krequest gauge
cloudcost_aws_s3_operation_by_location_usd_per_krequest{class="StandardStorage",region="ap-northeast-1",tier="1"} 1000
cloudcost_aws_s3_operation_by_location_usd_per_krequest{class="StandardStorage",region="ap-northeast-1",tier="2"} 1000
# HELP cloudcost_aws_s3_storage_by_location_usd_per_gibyte_hour Storage cost of S3 objects by region, class, and tier. Cost represented in USD/(GiB*h)
# TYPE cloudcost_aws_s3_storage_by_location_usd_per_gibyte_hour gauge
cloudcost_aws_s3_storage_by_location_usd_per_gibyte_hour{class="StandardStorage",region="ap-northeast-1"} 0.0013689253935660506
`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			client := mock_client.NewMockClient(ctrl)
			if tc.expectedResponse != 0 {
				client.EXPECT().Metrics().Return([]prometheus.Collector{})
			}
			if tc.GetBillingData != nil {
				client.EXPECT().
					GetBillingData(gomock.Any(), gomock.Any(), gomock.Any()).
					DoAndReturn(tc.GetBillingData).
					Times(1)
			}

			c := &Collector{
				client:     client,
				nextScrape: tc.nextScrape,
				metrics:    NewMetrics(),
			}
			up := c.CollectMetrics(nil)
			require.Equal(t, tc.expectedResponse, up)
			if tc.expectedResponse == 0 {
				return
			}

			r := prometheus.NewPedanticRegistry()
			err := c.Register(r)
			assert.NoError(t, err)

			err = testutil.CollectAndCompare(r, strings.NewReader(tc.expectedExposition), tc.metricNames...)
			assert.NoError(t, err)
		})
	}
}

func TestCollector_MultipleCalls(t *testing.T) {
	t.Run("Test multiple calls to the collect method", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		ce := mock_client.NewMockClient(ctrl)
		ce.EXPECT().
			GetBillingData(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(&client.BillingData{}, nil)

		c := &Collector{
			client:   ce,
			metrics:  NewMetrics(),
			interval: 1 * time.Hour,
		}
		up := c.CollectMetrics(nil)
		require.Equal(t, 1.0, up)

		up = c.CollectMetrics(nil)
		require.Equal(t, 1.0, up)
	})
	// This tests if the collect method is thread safe. If it fails, then we need to implement a mutex.`
	t.Run("Test multiple calls to collect method in parallel", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		ce := mock_client.NewMockClient(ctrl)
		getCostAndUsage := func(ctx context.Context, startDate time.Time, endDate time.Time) (*client.BillingData, error) {
			return &client.BillingData{Regions: map[string]*client.PricingModel{
				"ap-northeast-1": {
					Model: map[string]*client.Pricing{
						"APN1-Requests-Tier1": {
							Usage: 1,
							Units: "unit",
						},
						"APN1-Requests-Tier2": {
							Usage: 1,
							Units: "unit",
						},
						"APN1-TimedStorage": {
							Usage: 1,
							Units: "unit",
						},
						"PN1-unknown": {
							Usage: 1,
							Units: "unit",
						},
					},
				},
			}}, nil
		}

		goroutines := 10
		collectCalls := 1000
		ce.EXPECT().
			GetBillingData(gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(getCostAndUsage).
			Times(goroutines * collectCalls)

		c := &Collector{
			client:  ce,
			metrics: NewMetrics(),
		}

		for i := 0; i < goroutines; i++ {
			t.Run(fmt.Sprintf("Test %d", i), func(t *testing.T) {
				t.Parallel()
				for j := 0; j < collectCalls; j++ {
					up := c.CollectMetrics(nil)
					require.Equal(t, 1.0, up)
				}
			})
		}
	})
}
