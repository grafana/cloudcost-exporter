package s3

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	awscostexplorer "github.com/aws/aws-sdk-go-v2/service/costexplorer"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	mockcostexplorer "github.com/grafana/cloudcost-exporter/mocks/pkg/aws/costexplorer"
	"github.com/grafana/cloudcost-exporter/pkg/aws/costexplorer"
	mock_provider "github.com/grafana/cloudcost-exporter/pkg/provider/mocks"
)

func Test_getDimensionFromKey(t *testing.T) {
	f, err := os.Open("testdata/dimensions.csv")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	reader := csv.NewReader(f)
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatal(err)
	}

	for _, record := range records {
		key, want := record[0], record[2]
		if got := getComponentFromKey(key); got != want {
			t.Fatalf("getComponentFromKey(%s) = %v, want %v", key, got, want)
		}
	}
}

func Test_getRegionFromKey(t *testing.T) {
	f, err := os.Open("testdata/dimensions.csv")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	reader := csv.NewReader(f)
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatal(err)
	}

	for _, record := range records {
		key, want := record[0], record[1]
		got := getRegionFromKey(key)
		mappedWant := billingToRegionMap[want]
		if mappedWant != got {
			t.Fatalf("getRegionFromKey(%s) = %v, want %v", key, got, want)
		}
	}
}

func TestS3BillingData_AddRegion(t *testing.T) {
	type args struct {
		key   string
		group types.Group
	}
	tests := map[string]struct {
		args []args
		want int
	}{
		"Do not add a region if key is empty": {
			args: []args{
				{
					key: "",
					group: types.Group{
						Metrics: map[string]types.MetricValue{},
					},
				},
			},
			want: 0,
		},
		"Add a single region": {
			args: []args{
				{
					key: "USE2-Requests-Tier1",
					group: types.Group{
						Metrics: map[string]types.MetricValue{},
					},
				},
			},
			want: 1,
		},
		"Add multiple regions": {
			args: []args{
				{
					key: "USE2-Requests-Tier1",
					group: types.Group{
						Metrics: map[string]types.MetricValue{},
					},
				},
				{
					key: "USW1-Requests-Tier1",
					group: types.Group{
						Metrics: map[string]types.MetricValue{},
					},
				},
			},
			want: 2,
		},
		"Add multiple regions with duplicates": {
			args: []args{
				{
					key: "USE2-Requests-Tier1",
					group: types.Group{
						Metrics: map[string]types.MetricValue{},
					},
				},
				{
					key: "USE2-Requests-Tier1",
					group: types.Group{
						Metrics: map[string]types.MetricValue{},
					},
				},
				{
					key: "USW1-Requests-Tier1",
					group: types.Group{
						Metrics: map[string]types.MetricValue{},
					},
				},
				{
					key: "USW1-Requests-Tier1",
					group: types.Group{
						Metrics: map[string]types.MetricValue{},
					},
				},
			},
			want: 2,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			s := NewS3BillingData()
			for _, arg := range tt.args {
				region, dimension := getRegionFromKey(arg.key), getComponentFromKey(arg.key)
				s.AddMetricGroup(region, dimension, arg.group)
			}
			if len(s.Regions) != tt.want {
				t.Fatalf("len(s.Regions) = %v, want %d", len(s.Regions), tt.want)
			}
		})
	}
}

func TestNewCollector(t *testing.T) {
	type args struct {
		interval time.Duration
		client   costexplorer.CostExplorer
	}
	tests := map[string]struct {
		args  args
		want  *Collector
		error bool
	}{
		"Create a new collector": {
			args: args{
				interval: time.Duration(1) * time.Hour,
			},
			want:  &Collector{},
			error: false,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			c := mockcostexplorer.NewCostExplorer(t)

			got, err := New(tt.args.interval, c)
			if tt.error {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
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
	r.EXPECT().MustRegister(gomock.Any()).Times(5)

	c := &Collector{}
	err := c.Register(r)
	require.NoError(t, err)
}

func TestCollector_Collect(t *testing.T) {
	timeInPast := time.Now().Add(-48 * time.Hour)
	withoutNextScrape := []string{
		"aws_s3_storage_hourly_cost",
		"aws_s3_operations_cost",
		"aws_cost_exporter_requests_total",
		"aws_cost_exporter_request_errors_total",
	}

	for _, tc := range []struct {
		name             string
		nextScrape       time.Time
		GetCostAndUsage  func(ctx context.Context, params *awscostexplorer.GetCostAndUsageInput, optFns ...func(*awscostexplorer.Options)) (*awscostexplorer.GetCostAndUsageOutput, error)
		GetCostAndUsage2 func(ctx context.Context, params *awscostexplorer.GetCostAndUsageInput, optFns ...func(*awscostexplorer.Options)) (*awscostexplorer.GetCostAndUsageOutput, error)

		// metricNames can be nil to check all metrics, or a set of strings form an allow list of metrics to check.
		metricNames        []string
		expectedResponse   float64
		expectedExposition string
	}{
		{
			name:       "cost and usage error is bubbled-up",
			nextScrape: timeInPast,
			GetCostAndUsage: func(ctx context.Context, params *awscostexplorer.GetCostAndUsageInput, optFns ...func(*awscostexplorer.Options)) (*awscostexplorer.GetCostAndUsageOutput, error) {
				return nil, fmt.Errorf("test cost and usage error")
			},
			expectedResponse: 0.0,
		},
		{
			name:       "no cost and usage output",
			nextScrape: timeInPast,
			GetCostAndUsage: func(ctx context.Context, params *awscostexplorer.GetCostAndUsageInput, optFns ...func(*awscostexplorer.Options)) (*awscostexplorer.GetCostAndUsageOutput, error) {
				return &awscostexplorer.GetCostAndUsageOutput{}, nil
			},
			metricNames: withoutNextScrape,
			expectedExposition: `
# HELP aws_cost_exporter_request_errors_total Total number of errors when making requests to the AWS Cost Explorer API
# TYPE aws_cost_exporter_request_errors_total counter
aws_cost_exporter_request_errors_total 0
# HELP aws_cost_exporter_requests_total Total number of requests made to the AWS Cost Explorer API
# TYPE aws_cost_exporter_requests_total counter
aws_cost_exporter_requests_total 1
`,
			expectedResponse: 1.0,
		},
		{
			name:             "cost and usage output - one result without keys",
			nextScrape:       timeInPast,
			expectedResponse: 1.0,
			GetCostAndUsage: func(ctx context.Context, params *awscostexplorer.GetCostAndUsageInput, optFns ...func(*awscostexplorer.Options)) (*awscostexplorer.GetCostAndUsageOutput, error) {
				return &awscostexplorer.GetCostAndUsageOutput{
					ResultsByTime: []types.ResultByTime{{
						Groups: []types.Group{{
							Keys: nil,
						}},
					}},
				}, nil
			},
			metricNames: withoutNextScrape,
			expectedExposition: `
# HELP aws_cost_exporter_request_errors_total Total number of errors when making requests to the AWS Cost Explorer API
# TYPE aws_cost_exporter_request_errors_total counter
aws_cost_exporter_request_errors_total 0
# HELP aws_cost_exporter_requests_total Total number of requests made to the AWS Cost Explorer API
# TYPE aws_cost_exporter_requests_total counter
aws_cost_exporter_requests_total 1
`,
		},
		{
			name:             "cost and usage output - one result with keys but non-existent region",
			nextScrape:       timeInPast,
			expectedResponse: 1.0,
			GetCostAndUsage: func(ctx context.Context, params *awscostexplorer.GetCostAndUsageInput, optFns ...func(*awscostexplorer.Options)) (*awscostexplorer.GetCostAndUsageOutput, error) {
				return &awscostexplorer.GetCostAndUsageOutput{
					ResultsByTime: []types.ResultByTime{{
						Groups: []types.Group{{
							Keys: []string{"non-existent-region"},
						}},
					}},
				}, nil
			},
			metricNames: withoutNextScrape,
			expectedExposition: `
# HELP aws_cost_exporter_request_errors_total Total number of errors when making requests to the AWS Cost Explorer API
# TYPE aws_cost_exporter_request_errors_total counter
aws_cost_exporter_request_errors_total 0
# HELP aws_cost_exporter_requests_total Total number of requests made to the AWS Cost Explorer API
# TYPE aws_cost_exporter_requests_total counter
aws_cost_exporter_requests_total 1
`,
		},
		{
			name:             "cost and usage output - one result with keys but special-case region",
			nextScrape:       timeInPast,
			expectedResponse: 1.0,
			GetCostAndUsage: func(ctx context.Context, params *awscostexplorer.GetCostAndUsageInput, optFns ...func(*awscostexplorer.Options)) (*awscostexplorer.GetCostAndUsageOutput, error) {
				return &awscostexplorer.GetCostAndUsageOutput{
					ResultsByTime: []types.ResultByTime{{
						Groups: []types.Group{{
							Keys: []string{"Requests-Tier1", "Requests-Tier2"},
						}},
					}},
				}, nil
			},
			metricNames: withoutNextScrape,
			expectedExposition: `
# HELP aws_cost_exporter_request_errors_total Total number of errors when making requests to the AWS Cost Explorer API
# TYPE aws_cost_exporter_request_errors_total counter
aws_cost_exporter_request_errors_total 0
# HELP aws_cost_exporter_requests_total Total number of requests made to the AWS Cost Explorer API
# TYPE aws_cost_exporter_requests_total counter
aws_cost_exporter_requests_total 1
`,
		},
		{
			name:             "cost and usage output - one result with keys and valid region with a hyphen",
			nextScrape:       timeInPast,
			expectedResponse: 1.0,
			GetCostAndUsage: func(ctx context.Context, params *awscostexplorer.GetCostAndUsageInput, optFns ...func(*awscostexplorer.Options)) (*awscostexplorer.GetCostAndUsageOutput, error) {
				return &awscostexplorer.GetCostAndUsageOutput{
					ResultsByTime: []types.ResultByTime{{
						Groups: []types.Group{{
							// TODO: region lookup failure
							// TODO: test should fail
							Keys: []string{"AWS GovCloud (US-East)-Requests-Tier1"},
						}},
					}},
				}, nil
			},
			metricNames: withoutNextScrape,
			expectedExposition: `
# HELP aws_cost_exporter_request_errors_total Total number of errors when making requests to the AWS Cost Explorer API
# TYPE aws_cost_exporter_request_errors_total counter
aws_cost_exporter_request_errors_total 0
# HELP aws_cost_exporter_requests_total Total number of requests made to the AWS Cost Explorer API
# TYPE aws_cost_exporter_requests_total counter
aws_cost_exporter_requests_total 1
`,
		},
		{
			name:             "cost and usage output - three results with keys and valid region without a hyphen",
			nextScrape:       timeInPast,
			expectedResponse: 1.0,
			GetCostAndUsage: func(ctx context.Context, params *awscostexplorer.GetCostAndUsageInput, optFns ...func(*awscostexplorer.Options)) (*awscostexplorer.GetCostAndUsageOutput, error) {
				return &awscostexplorer.GetCostAndUsageOutput{
					ResultsByTime: []types.ResultByTime{
						{
							Groups: []types.Group{{
								Keys: []string{"APN1-Requests-Tier1"},
							}},
						},
						{
							Groups: []types.Group{{
								Keys: []string{"APN2-Requests-Tier2"},
							}},
						},
						{
							Groups: []types.Group{{
								Keys: []string{"APN3-TimedStorage"},
							}},
						},
					},
				}, nil
			},
			metricNames: withoutNextScrape,
			expectedExposition: `
# HELP aws_s3_operations_cost S3 operations cost per 1k requests
# TYPE aws_s3_operations_cost gauge
aws_s3_operations_cost{class="StandardStorage",region="ap-northeast-1",tier="1"} 0
aws_s3_operations_cost{class="StandardStorage",region="ap-northeast-2",tier="2"} 0
# HELP aws_s3_storage_hourly_cost S3 storage hourly cost in GiB
# TYPE aws_s3_storage_hourly_cost gauge
aws_s3_storage_hourly_cost{class="StandardStorage",region="ap-northeast-3"} 0
# HELP aws_cost_exporter_request_errors_total Total number of errors when making requests to the AWS Cost Explorer API
# TYPE aws_cost_exporter_request_errors_total counter
aws_cost_exporter_request_errors_total 0
# HELP aws_cost_exporter_requests_total Total number of requests made to the AWS Cost Explorer API
# TYPE aws_cost_exporter_requests_total counter
aws_cost_exporter_requests_total 1
`,
		},
		{
			name:             "cost and usage output - results with two pages",
			nextScrape:       timeInPast,
			expectedResponse: 1.0,
			GetCostAndUsage: func(ctx context.Context, params *awscostexplorer.GetCostAndUsageInput, optFns ...func(*awscostexplorer.Options)) (*awscostexplorer.GetCostAndUsageOutput, error) {
				t := "token"
				return &awscostexplorer.GetCostAndUsageOutput{
					ResultsByTime: []types.ResultByTime{{
						Groups: []types.Group{{
							Keys: []string{"APN1-Requests-Tier1"},
						}},
					}},
					NextPageToken: &t,
				}, nil
			},
			GetCostAndUsage2: func(ctx context.Context, params *awscostexplorer.GetCostAndUsageInput, optFns ...func(*awscostexplorer.Options)) (*awscostexplorer.GetCostAndUsageOutput, error) {
				return &awscostexplorer.GetCostAndUsageOutput{
					ResultsByTime: []types.ResultByTime{{
						Groups: []types.Group{{
							Keys: []string{"APN2-Requests-Tier2"},
						}},
					}},
				}, nil
			},
			metricNames: withoutNextScrape,
			expectedExposition: `
# HELP aws_s3_operations_cost S3 operations cost per 1k requests
# TYPE aws_s3_operations_cost gauge
aws_s3_operations_cost{class="StandardStorage",region="ap-northeast-1",tier="1"} 0
aws_s3_operations_cost{class="StandardStorage",region="ap-northeast-2",tier="2"} 0
# HELP aws_cost_exporter_request_errors_total Total number of errors when making requests to the AWS Cost Explorer API
# TYPE aws_cost_exporter_request_errors_total counter
aws_cost_exporter_request_errors_total 0
# HELP aws_cost_exporter_requests_total Total number of requests made to the AWS Cost Explorer API
# TYPE aws_cost_exporter_requests_total counter
aws_cost_exporter_requests_total 2
`,
		},
		{
			name:             "cost and usage output - result with nil amount",
			nextScrape:       timeInPast,
			expectedResponse: 1.0,
			GetCostAndUsage: func(ctx context.Context, params *awscostexplorer.GetCostAndUsageInput, optFns ...func(*awscostexplorer.Options)) (*awscostexplorer.GetCostAndUsageOutput, error) {
				return &awscostexplorer.GetCostAndUsageOutput{
					ResultsByTime: []types.ResultByTime{{
						Groups: []types.Group{{
							Keys: []string{"APN1-Requests-Tier1"},
							Metrics: map[string]types.MetricValue{
								"UsageQuantity": {},
								"UnblendedCost": {},
							},
						}},
					}},
				}, nil
			},
			metricNames: withoutNextScrape,
			expectedExposition: `
# HELP aws_s3_operations_cost S3 operations cost per 1k requests
# TYPE aws_s3_operations_cost gauge
aws_s3_operations_cost{class="StandardStorage",region="ap-northeast-1",tier="1"} 0
# HELP aws_cost_exporter_request_errors_total Total number of errors when making requests to the AWS Cost Explorer API
# TYPE aws_cost_exporter_request_errors_total counter
aws_cost_exporter_request_errors_total 0
# HELP aws_cost_exporter_requests_total Total number of requests made to the AWS Cost Explorer API
# TYPE aws_cost_exporter_requests_total counter
aws_cost_exporter_requests_total 1
`,
		},
		{
			name:             "cost and usage output - result with invalid amount",
			nextScrape:       timeInPast,
			expectedResponse: 1.0,
			GetCostAndUsage: func(ctx context.Context, params *awscostexplorer.GetCostAndUsageInput, optFns ...func(*awscostexplorer.Options)) (*awscostexplorer.GetCostAndUsageOutput, error) {
				a := ""
				return &awscostexplorer.GetCostAndUsageOutput{
					ResultsByTime: []types.ResultByTime{{
						Groups: []types.Group{{
							Keys: []string{"APN1-Requests-Tier1"},
							Metrics: map[string]types.MetricValue{
								"UsageQuantity": {Amount: &a},
								"UnblendedCost": {Amount: &a},
							},
						}},
					}},
				}, nil
			},
			metricNames: withoutNextScrape,
			expectedExposition: `
# HELP aws_s3_operations_cost S3 operations cost per 1k requests
# TYPE aws_s3_operations_cost gauge
aws_s3_operations_cost{class="StandardStorage",region="ap-northeast-1",tier="1"} 0
# HELP aws_cost_exporter_request_errors_total Total number of errors when making requests to the AWS Cost Explorer API
# TYPE aws_cost_exporter_request_errors_total counter
aws_cost_exporter_request_errors_total 0
# HELP aws_cost_exporter_requests_total Total number of requests made to the AWS Cost Explorer API
# TYPE aws_cost_exporter_requests_total counter
aws_cost_exporter_requests_total 1
`,
		},
		{
			name:             "cost and usage output - result with nil unit",
			nextScrape:       timeInPast,
			expectedResponse: 1.0,
			GetCostAndUsage: func(ctx context.Context, params *awscostexplorer.GetCostAndUsageInput, optFns ...func(*awscostexplorer.Options)) (*awscostexplorer.GetCostAndUsageOutput, error) {
				a := "1"
				return &awscostexplorer.GetCostAndUsageOutput{
					ResultsByTime: []types.ResultByTime{{
						Groups: []types.Group{{
							Keys: []string{"APN1-Requests-Tier1"},
							Metrics: map[string]types.MetricValue{
								"UsageQuantity": {Amount: &a},
								"UnblendedCost": {Amount: &a},
							},
						}},
					}},
				}, nil
			},
			metricNames: withoutNextScrape,
			expectedExposition: `
# HELP aws_s3_operations_cost S3 operations cost per 1k requests
# TYPE aws_s3_operations_cost gauge
aws_s3_operations_cost{class="StandardStorage",region="ap-northeast-1",tier="1"} 1000
# HELP aws_cost_exporter_request_errors_total Total number of errors when making requests to the AWS Cost Explorer API
# TYPE aws_cost_exporter_request_errors_total counter
aws_cost_exporter_request_errors_total 0
# HELP aws_cost_exporter_requests_total Total number of requests made to the AWS Cost Explorer API
# TYPE aws_cost_exporter_requests_total counter
aws_cost_exporter_requests_total 1
`,
		},
		{
			name:             "cost and usage output - result with valid amount and unit",
			nextScrape:       timeInPast,
			expectedResponse: 1.0,
			GetCostAndUsage: func(ctx context.Context, params *awscostexplorer.GetCostAndUsageInput, optFns ...func(*awscostexplorer.Options)) (*awscostexplorer.GetCostAndUsageOutput, error) {
				a := "1"
				u := "unit"
				return &awscostexplorer.GetCostAndUsageOutput{
					ResultsByTime: []types.ResultByTime{
						{
							Groups: []types.Group{{
								Keys: []string{"APN1-Requests-Tier1"},
								Metrics: map[string]types.MetricValue{
									"UsageQuantity": {Amount: &a, Unit: &u},
									"UnblendedCost": {Amount: &a, Unit: &u},
								},
							}},
						},
						{
							Groups: []types.Group{{
								Keys: []string{"APN1-Requests-Tier2"},
								Metrics: map[string]types.MetricValue{
									"UsageQuantity": {Amount: &a, Unit: &u},
									"UnblendedCost": {Amount: &a, Unit: &u},
								},
							}},
						},
						{
							Groups: []types.Group{{
								Keys: []string{"APN1-TimedStorage"},
								Metrics: map[string]types.MetricValue{
									"UsageQuantity": {Amount: &a, Unit: &u},
									"UnblendedCost": {Amount: &a, Unit: &u},
								},
							}},
						},
						{
							Groups: []types.Group{{
								Keys: []string{"APN1-unknown"},
								Metrics: map[string]types.MetricValue{
									"UsageQuantity": {Amount: &a, Unit: &u},
									"UnblendedCost": {Amount: &a, Unit: &u},
								},
							}},
						},
					},
				}, nil
			},
			metricNames: withoutNextScrape,
			expectedExposition: `
# HELP aws_cost_exporter_request_errors_total Total number of errors when making requests to the AWS Cost Explorer API
# TYPE aws_cost_exporter_request_errors_total counter
aws_cost_exporter_request_errors_total 0
# HELP aws_cost_exporter_requests_total Total number of requests made to the AWS Cost Explorer API
# TYPE aws_cost_exporter_requests_total counter
aws_cost_exporter_requests_total 1
# HELP aws_s3_operations_cost S3 operations cost per 1k requests
# TYPE aws_s3_operations_cost gauge
aws_s3_operations_cost{class="StandardStorage",region="ap-northeast-1",tier="1"} 1000
aws_s3_operations_cost{class="StandardStorage",region="ap-northeast-1",tier="2"} 1000
# HELP aws_s3_storage_hourly_cost S3 storage hourly cost in GiB
# TYPE aws_s3_storage_hourly_cost gauge
aws_s3_storage_hourly_cost{class="StandardStorage",region="ap-northeast-1"} 0.0013689253935660506
`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ce := mockcostexplorer.NewCostExplorer(t)
			if tc.GetCostAndUsage != nil {
				ce.EXPECT().
					GetCostAndUsage(mock.Anything, mock.Anything, mock.Anything).
					RunAndReturn(tc.GetCostAndUsage).
					Once()
			}
			if tc.GetCostAndUsage2 != nil {
				ce.EXPECT().
					GetCostAndUsage(mock.Anything, mock.Anything, mock.Anything).
					RunAndReturn(tc.GetCostAndUsage2).
					Once()
			}

			c := &Collector{
				client:     ce,
				nextScrape: tc.nextScrape,
				metrics:    NewMetrics(),
			}
			up := c.Collect()
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

func Test_unitCostForComponent(t *testing.T) {
	tests := map[string]struct {
		component string
		pricing   *Pricing
		want      float64
	}{
		"Requests-Tier1 basic": {
			component: "Requests-Tier1",
			pricing: &Pricing{
				Usage: 1.0,
				Cost:  1.0,
			},
			want: 1000,
		},
		"Requests-Tier1 with 1000's of requests": {
			component: "Requests-Tier1",
			pricing: &Pricing{
				Usage: 1000.0,
				Cost:  1.0,
			},
			want: 1,
		},
		"Requests-Tier1 with 1000's of costs": {
			component: "Requests-Tier1",
			pricing: &Pricing{
				Usage: 1.0,
				Cost:  1000.0,
			},
			want: 1e6,
		},
		"Requests-Tier1 with 1000's of costs and 1000 requests": {
			component: "Requests-Tier1",
			pricing: &Pricing{
				Usage: 1000.0,
				Cost:  1000.0,
			},
			want: 1000,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equalf(t, tt.want, unitCostForComponent(tt.component, tt.pricing), "unitCostForComponent(%v, %v)", tt.component, tt.pricing)
		})
	}
}

func TestCollector_MultipleCalls(t *testing.T) {
	t.Run("Test multiple calls to the collect method", func(t *testing.T) {
		ce := mockcostexplorer.NewCostExplorer(t)
		ce.EXPECT().
			GetCostAndUsage(mock.Anything, mock.Anything, mock.Anything).
			Return(&awscostexplorer.GetCostAndUsageOutput{}, nil)

		c := &Collector{
			client:   ce,
			metrics:  NewMetrics(),
			interval: 1 * time.Hour,
		}
		up := c.Collect()
		require.Equal(t, 1.0, up)

		up = c.Collect()
		require.Equal(t, 1.0, up)
	})
	// This tests if the collect method is thread safe. If it fails, then we need to implement a mutex.`
	t.Run("Test multiple calls to collect method in parallel", func(t *testing.T) {
		ce := mockcostexplorer.NewCostExplorer(t)
		getCostAndUsage := func(ctx context.Context, params *awscostexplorer.GetCostAndUsageInput, optFns ...func(*awscostexplorer.Options)) (*awscostexplorer.GetCostAndUsageOutput, error) {
			a := "1"
			u := "unit"
			return &awscostexplorer.GetCostAndUsageOutput{
				ResultsByTime: []types.ResultByTime{
					{
						Groups: []types.Group{{
							Keys: []string{"APN1-Requests-Tier1"},
							Metrics: map[string]types.MetricValue{
								"UsageQuantity": {Amount: &a, Unit: &u},
								"UnblendedCost": {Amount: &a, Unit: &u},
							},
						}},
					},
					{
						Groups: []types.Group{{
							Keys: []string{"APN1-Requests-Tier2"},
							Metrics: map[string]types.MetricValue{
								"UsageQuantity": {Amount: &a, Unit: &u},
								"UnblendedCost": {Amount: &a, Unit: &u},
							},
						}},
					},
					{
						Groups: []types.Group{{
							Keys: []string{"APN1-TimedStorage"},
							Metrics: map[string]types.MetricValue{
								"UsageQuantity": {Amount: &a, Unit: &u},
								"UnblendedCost": {Amount: &a, Unit: &u},
							},
						}},
					},
					{
						Groups: []types.Group{{
							Keys: []string{"APN1-unknown"},
							Metrics: map[string]types.MetricValue{
								"UsageQuantity": {Amount: &a, Unit: &u},
								"UnblendedCost": {Amount: &a, Unit: &u},
							},
						}},
					},
				},
			}, nil
		}
		goroutines := 10
		collectCalls := 1000
		ce.EXPECT().
			GetCostAndUsage(mock.Anything, mock.Anything, mock.Anything).
			RunAndReturn(getCostAndUsage).
			Times(goroutines * collectCalls)

		c := &Collector{
			client:  ce,
			metrics: NewMetrics(),
		}

		for i := 0; i < goroutines; i++ {
			t.Run(fmt.Sprintf("Test %d", i), func(t *testing.T) {
				t.Parallel()
				for j := 0; j < collectCalls; j++ {
					up := c.Collect()
					require.Equal(t, 1.0, up)
				}
			})
		}
	})
}
