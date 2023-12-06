package s3

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"testing"
	"time"

	awscostexplorer "github.com/aws/aws-sdk-go-v2/service/costexplorer"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
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
	timeInFuture := time.Now().Add(48 * time.Hour)

	for _, tc := range []struct {
		name             string
		nextScrape       time.Time
		GetCostAndUsage  func(ctx context.Context, params *awscostexplorer.GetCostAndUsageInput, optFns ...func(*awscostexplorer.Options)) (*awscostexplorer.GetCostAndUsageOutput, error)
		GetCostAndUsage2 func(ctx context.Context, params *awscostexplorer.GetCostAndUsageInput, optFns ...func(*awscostexplorer.Options)) (*awscostexplorer.GetCostAndUsageOutput, error)
		expectedError    error
	}{
		{
			name:       "skip collection",
			nextScrape: timeInFuture,
		},
		{
			name:       "cost and usage error is bubbled-up",
			nextScrape: timeInPast,
			GetCostAndUsage: func(ctx context.Context, params *awscostexplorer.GetCostAndUsageInput, optFns ...func(*awscostexplorer.Options)) (*awscostexplorer.GetCostAndUsageOutput, error) {
				return nil, fmt.Errorf("test cost and usage error")
			},
			expectedError: fmt.Errorf("test cost and usage error"),
		},
		{
			name:       "no cost and usage output",
			nextScrape: timeInPast,
			GetCostAndUsage: func(ctx context.Context, params *awscostexplorer.GetCostAndUsageInput, optFns ...func(*awscostexplorer.Options)) (*awscostexplorer.GetCostAndUsageOutput, error) {
				return &awscostexplorer.GetCostAndUsageOutput{}, nil
			},
		},
		{
			name:       "cost and usage output - one result without keys",
			nextScrape: timeInPast,
			GetCostAndUsage: func(ctx context.Context, params *awscostexplorer.GetCostAndUsageInput, optFns ...func(*awscostexplorer.Options)) (*awscostexplorer.GetCostAndUsageOutput, error) {
				return &awscostexplorer.GetCostAndUsageOutput{
					ResultsByTime: []types.ResultByTime{{
						Groups: []types.Group{{
							Keys: nil,
						}},
					}},
				}, nil
			},
		},
		{
			name:       "cost and usage output - one result with keys but non-existent region",
			nextScrape: timeInPast,
			GetCostAndUsage: func(ctx context.Context, params *awscostexplorer.GetCostAndUsageInput, optFns ...func(*awscostexplorer.Options)) (*awscostexplorer.GetCostAndUsageOutput, error) {
				return &awscostexplorer.GetCostAndUsageOutput{
					ResultsByTime: []types.ResultByTime{{
						Groups: []types.Group{{
							Keys: []string{"non-existent-region"},
						}},
					}},
				}, nil
			},
		},
		{
			name:       "cost and usage output - one result with keys but special-case region",
			nextScrape: timeInPast,
			GetCostAndUsage: func(ctx context.Context, params *awscostexplorer.GetCostAndUsageInput, optFns ...func(*awscostexplorer.Options)) (*awscostexplorer.GetCostAndUsageOutput, error) {
				return &awscostexplorer.GetCostAndUsageOutput{
					ResultsByTime: []types.ResultByTime{{
						Groups: []types.Group{{
							Keys: []string{"Requests-Tier1", "Requests-Tier2"},
						}},
					}},
				}, nil
			},
		},
		{
			name:       "cost and usage output - one result with keys and valid region with a hyphen",
			nextScrape: timeInPast,
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
		},
		{
			name:       "cost and usage output - three results with keys and valid region without a hyphen",
			nextScrape: timeInPast,
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
		},
		{
			name:       "cost and usage output - results with two pages",
			nextScrape: timeInPast,
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
		},
		{
			name:       "cost and usage output - result with nil amount",
			nextScrape: timeInPast,
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
		},
		{
			name:       "cost and usage output - result with invalid amount",
			nextScrape: timeInPast,
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
		},
		{
			name:       "cost and usage output - result with nil unit",
			nextScrape: timeInPast,
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
		},
		{
			name:       "cost and usage output - result with valid amount and unit",
			nextScrape: timeInPast,
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
			}
			err := c.Collect()
			if tc.expectedError != nil {
				require.EqualError(t, err, tc.expectedError.Error())
				return
			}
			require.NoError(t, err)
		})
	}
}
