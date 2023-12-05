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

func TestExportBillingData(t *testing.T) {
	for _, tc := range []struct {
		name            string
		GetCostAndUsage func(ctx context.Context, params *awscostexplorer.GetCostAndUsageInput, optFns ...func(*awscostexplorer.Options)) (*awscostexplorer.GetCostAndUsageOutput, error)
		expectedError   error
	}{
		{
			name: "error",
			GetCostAndUsage: func(ctx context.Context, params *awscostexplorer.GetCostAndUsageInput, optFns ...func(*awscostexplorer.Options)) (*awscostexplorer.GetCostAndUsageOutput, error) {
				return nil, fmt.Errorf("test cost and usage error")
			},
			expectedError: fmt.Errorf("test cost and usage error"),
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

			err := ExportBillingData(ce)
			if tc.expectedError != nil {
				require.EqualError(t, err, tc.expectedError.Error())
				return
			}
			require.NoError(t, err)
		})
	}
}
