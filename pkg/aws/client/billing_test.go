package client

import (
	"context"
	"encoding/csv"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
	"github.com/grafana/cloudcost-exporter/pkg/aws/services/mocks"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
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
		mappedWant := BillingToRegionMap[want]
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
			s := BillingData{Regions: make(map[string]*PricingModel)}
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

func Test_getBillingData_Metrics(t *testing.T) {

	for _, tc := range []struct {
		name             string
		expectedErr      error
		GetCostAndUsage  func(ctx context.Context, params *costexplorer.GetCostAndUsageInput, optFns ...func(*costexplorer.Options)) (*costexplorer.GetCostAndUsageOutput, error)
		GetCostAndUsage2 func(ctx context.Context, params *costexplorer.GetCostAndUsageInput, optFns ...func(*costexplorer.Options)) (*costexplorer.GetCostAndUsageOutput, error)

		// metricNames can be nil to check all metrics, or a set of strings form an allow list of metrics to check.
		metricNames        []string
		expectedExposition string
	}{
		{
			name: "no cost and usage output",
			GetCostAndUsage: func(ctx context.Context, params *costexplorer.GetCostAndUsageInput, optFns ...func(*costexplorer.Options)) (*costexplorer.GetCostAndUsageOutput, error) {
				return &costexplorer.GetCostAndUsageOutput{}, nil
			},
			metricNames: []string{
				"cloudcost_exporter_aws_s3_cost_api_requests_total",
				"cloudcost_exporter_aws_s3_cost_api_requests_errors_total",
			},
			expectedExposition: `
# HELP cloudcost_exporter_aws_s3_cost_api_requests_errors_total Total number of errors when making requests to the AWS Cost Explorer API
# TYPE cloudcost_exporter_aws_s3_cost_api_requests_errors_total counter
cloudcost_exporter_aws_s3_cost_api_requests_errors_total 0
# HELP cloudcost_exporter_aws_s3_cost_api_requests_total Total number of requests made to the AWS Cost Explorer API
# TYPE cloudcost_exporter_aws_s3_cost_api_requests_total counter
cloudcost_exporter_aws_s3_cost_api_requests_total 1
`,
		},
		{
			name: "cost and usage output - one result without keys",
			GetCostAndUsage: func(ctx context.Context, params *costexplorer.GetCostAndUsageInput, optFns ...func(*costexplorer.Options)) (*costexplorer.GetCostAndUsageOutput, error) {
				return &costexplorer.GetCostAndUsageOutput{
					ResultsByTime: []types.ResultByTime{{
						Groups: []types.Group{{
							Keys: nil,
						}},
					}},
				}, nil
			},
			metricNames: []string{
				"cloudcost_exporter_aws_s3_cost_api_requests_total",
				"cloudcost_exporter_aws_s3_cost_api_requests_errors_total",
			},
			expectedExposition: `
# HELP cloudcost_exporter_aws_s3_cost_api_requests_errors_total Total number of errors when making requests to the AWS Cost Explorer API
# TYPE cloudcost_exporter_aws_s3_cost_api_requests_errors_total counter
cloudcost_exporter_aws_s3_cost_api_requests_errors_total 0
# HELP cloudcost_exporter_aws_s3_cost_api_requests_total Total number of requests made to the AWS Cost Explorer API
# TYPE cloudcost_exporter_aws_s3_cost_api_requests_total counter
cloudcost_exporter_aws_s3_cost_api_requests_total 1
`,
		},
		{
			name: "cost and usage output - one result with keys but non-existent region",
			GetCostAndUsage: func(ctx context.Context, params *costexplorer.GetCostAndUsageInput, optFns ...func(*costexplorer.Options)) (*costexplorer.GetCostAndUsageOutput, error) {
				return &costexplorer.GetCostAndUsageOutput{
					ResultsByTime: []types.ResultByTime{{
						Groups: []types.Group{{
							Keys: []string{"non-existent-region"},
						}},
					}},
				}, nil
			},
			metricNames: []string{
				"cloudcost_exporter_aws_s3_cost_api_requests_total",
				"cloudcost_exporter_aws_s3_cost_api_requests_errors_total",
			},
			expectedExposition: `
# HELP cloudcost_exporter_aws_s3_cost_api_requests_errors_total Total number of errors when making requests to the AWS Cost Explorer API
# TYPE cloudcost_exporter_aws_s3_cost_api_requests_errors_total counter
cloudcost_exporter_aws_s3_cost_api_requests_errors_total 0
# HELP cloudcost_exporter_aws_s3_cost_api_requests_total Total number of requests made to the AWS Cost Explorer API
# TYPE cloudcost_exporter_aws_s3_cost_api_requests_total counter
cloudcost_exporter_aws_s3_cost_api_requests_total 1
`,
		},
		{
			name: "cost and usage output - one result with keys but special-case region",
			GetCostAndUsage: func(ctx context.Context, params *costexplorer.GetCostAndUsageInput, optFns ...func(*costexplorer.Options)) (*costexplorer.GetCostAndUsageOutput, error) {
				return &costexplorer.GetCostAndUsageOutput{
					ResultsByTime: []types.ResultByTime{{
						Groups: []types.Group{{
							Keys: []string{"Requests-Tier1", "Requests-Tier2"},
						}},
					}},
				}, nil
			},
			metricNames: []string{
				"cloudcost_exporter_aws_s3_cost_api_requests_total",
				"cloudcost_exporter_aws_s3_cost_api_requests_errors_total",
			},
			expectedExposition: `
# HELP cloudcost_exporter_aws_s3_cost_api_requests_errors_total Total number of errors when making requests to the AWS Cost Explorer API
# TYPE cloudcost_exporter_aws_s3_cost_api_requests_errors_total counter
cloudcost_exporter_aws_s3_cost_api_requests_errors_total 0
# HELP cloudcost_exporter_aws_s3_cost_api_requests_total Total number of requests made to the AWS Cost Explorer API
# TYPE cloudcost_exporter_aws_s3_cost_api_requests_total counter
cloudcost_exporter_aws_s3_cost_api_requests_total 1
`,
		},
		{
			name: "cost and usage output - one result with keys and valid region with a hyphen",
			GetCostAndUsage: func(ctx context.Context, params *costexplorer.GetCostAndUsageInput, optFns ...func(*costexplorer.Options)) (*costexplorer.GetCostAndUsageOutput, error) {
				return &costexplorer.GetCostAndUsageOutput{
					ResultsByTime: []types.ResultByTime{{
						Groups: []types.Group{{
							// TODO: region lookup failure
							// TODO: test should fail
							Keys: []string{"AWS GovCloud (US-East)-Requests-Tier1"},
						}},
					}},
				}, nil
			},
			metricNames: []string{
				"cloudcost_exporter_aws_s3_cost_api_requests_total",
				"cloudcost_exporter_aws_s3_cost_api_requests_errors_total",
			},
			expectedExposition: `
# HELP cloudcost_exporter_aws_s3_cost_api_requests_errors_total Total number of errors when making requests to the AWS Cost Explorer API
# TYPE cloudcost_exporter_aws_s3_cost_api_requests_errors_total counter
cloudcost_exporter_aws_s3_cost_api_requests_errors_total 0
# HELP cloudcost_exporter_aws_s3_cost_api_requests_total Total number of requests made to the AWS Cost Explorer API
# TYPE cloudcost_exporter_aws_s3_cost_api_requests_total counter
cloudcost_exporter_aws_s3_cost_api_requests_total 1
`,
		},
		{
			name: "cost and usage output - results with two pages",
			GetCostAndUsage: func(ctx context.Context, params *costexplorer.GetCostAndUsageInput, optFns ...func(*costexplorer.Options)) (*costexplorer.GetCostAndUsageOutput, error) {
				t := "token"
				return &costexplorer.GetCostAndUsageOutput{
					ResultsByTime: []types.ResultByTime{{
						Groups: []types.Group{{
							Keys: []string{"APN1-Requests-Tier1"},
						}},
					}},
					NextPageToken: &t,
				}, nil
			},
			GetCostAndUsage2: func(ctx context.Context, params *costexplorer.GetCostAndUsageInput, optFns ...func(*costexplorer.Options)) (*costexplorer.GetCostAndUsageOutput, error) {
				return &costexplorer.GetCostAndUsageOutput{
					ResultsByTime: []types.ResultByTime{{
						Groups: []types.Group{{
							Keys: []string{"APN2-Requests-Tier2"},
						}},
					}},
				}, nil
			},
			metricNames: []string{
				"cloudcost_exporter_aws_s3_cost_api_requests_total",
				"cloudcost_exporter_aws_s3_cost_api_requests_errors_total",
			},
			expectedExposition: `
# HELP cloudcost_exporter_aws_s3_cost_api_requests_errors_total Total number of errors when making requests to the AWS Cost Explorer API
# TYPE cloudcost_exporter_aws_s3_cost_api_requests_errors_total counter
cloudcost_exporter_aws_s3_cost_api_requests_errors_total 0
# HELP cloudcost_exporter_aws_s3_cost_api_requests_total Total number of requests made to the AWS Cost Explorer API
# TYPE cloudcost_exporter_aws_s3_cost_api_requests_total counter
cloudcost_exporter_aws_s3_cost_api_requests_total 2
`,
		},
		{
			name: "cost and usage output - result with nil amount",
			GetCostAndUsage: func(ctx context.Context, params *costexplorer.GetCostAndUsageInput, optFns ...func(*costexplorer.Options)) (*costexplorer.GetCostAndUsageOutput, error) {
				return &costexplorer.GetCostAndUsageOutput{
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
			metricNames: []string{
				"cloudcost_exporter_aws_s3_cost_api_requests_total",
				"cloudcost_exporter_aws_s3_cost_api_requests_errors_total",
			},
			expectedExposition: `
# HELP cloudcost_exporter_aws_s3_cost_api_requests_errors_total Total number of errors when making requests to the AWS Cost Explorer API
# TYPE cloudcost_exporter_aws_s3_cost_api_requests_errors_total counter
cloudcost_exporter_aws_s3_cost_api_requests_errors_total 0
# HELP cloudcost_exporter_aws_s3_cost_api_requests_total Total number of requests made to the AWS Cost Explorer API
# TYPE cloudcost_exporter_aws_s3_cost_api_requests_total counter
cloudcost_exporter_aws_s3_cost_api_requests_total 1
`,
		},
		{
			name: "cost and usage output - result with invalid amount",
			GetCostAndUsage: func(ctx context.Context, params *costexplorer.GetCostAndUsageInput, optFns ...func(*costexplorer.Options)) (*costexplorer.GetCostAndUsageOutput, error) {
				a := ""
				return &costexplorer.GetCostAndUsageOutput{
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
			metricNames: []string{
				"cloudcost_exporter_aws_s3_cost_api_requests_total",
				"cloudcost_exporter_aws_s3_cost_api_requests_errors_total",
			},
			expectedExposition: `
# HELP cloudcost_exporter_aws_s3_cost_api_requests_errors_total Total number of errors when making requests to the AWS Cost Explorer API
# TYPE cloudcost_exporter_aws_s3_cost_api_requests_errors_total counter
cloudcost_exporter_aws_s3_cost_api_requests_errors_total 0
# HELP cloudcost_exporter_aws_s3_cost_api_requests_total Total number of requests made to the AWS Cost Explorer API
# TYPE cloudcost_exporter_aws_s3_cost_api_requests_total counter
cloudcost_exporter_aws_s3_cost_api_requests_total 1
`,
		},
		{
			name: "cost and usage output - result with nil unit",
			GetCostAndUsage: func(ctx context.Context, params *costexplorer.GetCostAndUsageInput, optFns ...func(*costexplorer.Options)) (*costexplorer.GetCostAndUsageOutput, error) {
				a := "1"
				return &costexplorer.GetCostAndUsageOutput{
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
			metricNames: []string{
				"cloudcost_exporter_aws_s3_cost_api_requests_total",
				"cloudcost_exporter_aws_s3_cost_api_requests_errors_total",
			},
			expectedExposition: `
# HELP cloudcost_exporter_aws_s3_cost_api_requests_errors_total Total number of errors when making requests to the AWS Cost Explorer API
# TYPE cloudcost_exporter_aws_s3_cost_api_requests_errors_total counter
cloudcost_exporter_aws_s3_cost_api_requests_errors_total 0
# HELP cloudcost_exporter_aws_s3_cost_api_requests_total Total number of requests made to the AWS Cost Explorer API
# TYPE cloudcost_exporter_aws_s3_cost_api_requests_total counter
cloudcost_exporter_aws_s3_cost_api_requests_total 1
`,
		},
		{
			name: "cost and usage output - result with valid amount and unit",
			GetCostAndUsage: func(ctx context.Context, params *costexplorer.GetCostAndUsageInput, optFns ...func(*costexplorer.Options)) (*costexplorer.GetCostAndUsageOutput, error) {
				a := "1"
				u := "unit"
				return &costexplorer.GetCostAndUsageOutput{
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
			metricNames: []string{
				"cloudcost_exporter_aws_s3_cost_api_requests_total",
				"cloudcost_exporter_aws_s3_cost_api_requests_errors_total",
			},
			expectedExposition: `
# HELP cloudcost_exporter_aws_s3_cost_api_requests_errors_total Total number of errors when making requests to the AWS Cost Explorer API
# TYPE cloudcost_exporter_aws_s3_cost_api_requests_errors_total counter
cloudcost_exporter_aws_s3_cost_api_requests_errors_total 0
# HELP cloudcost_exporter_aws_s3_cost_api_requests_total Total number of requests made to the AWS Cost Explorer API
# TYPE cloudcost_exporter_aws_s3_cost_api_requests_total counter
cloudcost_exporter_aws_s3_cost_api_requests_total 1
`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			costExplorer := mocks.NewMockCostExplorer(ctrl)
			if tc.GetCostAndUsage != nil {
				costExplorer.EXPECT().
					GetCostAndUsage(gomock.Any(), gomock.Any(), gomock.Any()).
					DoAndReturn(tc.GetCostAndUsage).
					Times(1)
			}

			if tc.GetCostAndUsage2 != nil {
				costExplorer.EXPECT().
					GetCostAndUsage(gomock.Any(), gomock.Any(), gomock.Any()).
					DoAndReturn(tc.GetCostAndUsage2).
					Times(1)
			}

			m := NewMetrics()
			r := prometheus.NewPedanticRegistry()
			r.MustRegister(m.RequestCount, m.RequestErrorsCount)
			b := newBilling(costExplorer, m)
			_, _ = b.getBillingData(t.Context(), time.Now(), time.Now())

			err := testutil.CollectAndCompare(r, strings.NewReader(tc.expectedExposition), tc.metricNames...)
			assert.NoError(t, err)
		})
	}
}
