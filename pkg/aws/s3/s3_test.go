package s3

import (
	"encoding/csv"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
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
		region   string
		profile  string
		interval time.Duration
	}
	tests := map[string]struct {
		args  args
		want  *Collector
		error bool
	}{
		"Create a new collector": {
			args: args{
				region:   "us-east-1",
				profile:  "workloads-dev",
				interval: time.Duration(1) * time.Hour,
			},
			want:  &Collector{},
			error: true,
		},
		"Expect an error when creating a Collector with no profile outside of EC2": {
			args: args{
				region:   "us-east-1",
				profile:  "",
				interval: time.Duration(1) * time.Hour,
			},
			want:  &Collector{},
			error: true,
		},
		"Expect an error when creating a Collector with no region outside of EC2": {
			args: args{
				region:   "",
				profile:  "workloads-dev",
				interval: time.Duration(1) * time.Hour,
			},
			want:  &Collector{},
			error: true,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := NewCollector(tt.args.region, tt.args.profile, tt.args.interval)
			if err != nil && !tt.error {
				t.Errorf("NewCollector() error = %v", err)
				return
			}
			if got == nil {
				t.Errorf("NewCollector() = %v, want %v", got, tt.want)
			}
		})
	}
}
