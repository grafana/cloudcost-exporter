package aws

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/grafana/cloudcost-exporter/pkg/aws/client"
	mock_client "github.com/grafana/cloudcost-exporter/pkg/aws/client/mocks"
	"github.com/grafana/cloudcost-exporter/pkg/provider"
	mock_provider "github.com/grafana/cloudcost-exporter/pkg/provider/mocks"
	"github.com/grafana/cloudcost-exporter/pkg/utils"
)

var logger = slog.New(slog.NewTextHandler(os.Stdout, nil))

// mockRegionClient is a minimal client mock for testing AWS collector creation
type mockRegionClient struct {
	client.Client
}

func (m *mockRegionClient) ListOnDemandPrices(ctx context.Context, region string) ([]string, error) {
	return []string{}, nil
}

func (m *mockRegionClient) ListSpotPrices(ctx context.Context) ([]types.SpotPrice, error) {
	return []types.SpotPrice{}, nil
}

func (m *mockRegionClient) ListStoragePrices(ctx context.Context, region string) ([]string, error) {
	return []string{}, nil
}

// Test_NewWithDependencies tests the newWithDependencies function with mock clients.
// This tests the core logic of New() without requiring AWS credentials or network access.
func Test_NewWithDependencies(t *testing.T) {
	tests := []struct {
		name               string
		services           []string
		regions            []types.Region
		setupMockClient    func(*mock_client.MockClient)
		setupRegionClients map[string]client.Client
		expectedCollectors int
		expectedError      string
		validateAWS        func(t *testing.T, aws *AWS)
	}{
		{
			name:     "empty services list creates no collectors",
			services: []string{},
			regions: []types.Region{
				{RegionName: stringPtr("us-east-1")},
			},
			setupRegionClients: map[string]client.Client{},
			expectedCollectors: 0,
			validateAWS: func(t *testing.T, aws *AWS) {
				assert.NotNil(t, aws)
				assert.NotNil(t, aws.Config)
				assert.NotNil(t, aws.logger)
				assert.Equal(t, 0, len(aws.collectors))
			},
		},
		{
			name:     "single S3 service creates S3 collector",
			services: []string{"S3"},
			regions: []types.Region{
				{RegionName: stringPtr("us-east-1")},
			},
			setupMockClient: func(m *mock_client.MockClient) {
				m.EXPECT().DescribeRegions(gomock.Any(), false).Return(nil, nil)
			},
			setupRegionClients: map[string]client.Client{},
			expectedCollectors: 1,
			validateAWS: func(t *testing.T, aws *AWS) {
				assert.Equal(t, 1, len(aws.collectors))
			},
		},
		{
			name:     "multiple services create multiple collectors",
			services: []string{"S3", "EC2"},
			regions: []types.Region{
				{RegionName: stringPtr("us-east-1")},
				{RegionName: stringPtr("us-west-2")},
			},
			setupMockClient: func(m *mock_client.MockClient) {
				m.EXPECT().DescribeRegions(gomock.Any(), false).Return(nil, nil)
			},
			setupRegionClients: map[string]client.Client{
				"us-east-1": &mockRegionClient{}, // EC2 collector needs region map
				"us-west-2": &mockRegionClient{},
			},
			expectedCollectors: 2,
			validateAWS: func(t *testing.T, aws *AWS) {
				assert.Equal(t, 2, len(aws.collectors))
			},
		},
		{
			name:     "unknown service is skipped",
			services: []string{"UNKNOWN_SERVICE"},
			regions: []types.Region{
				{RegionName: stringPtr("us-east-1")},
			},
			setupRegionClients: map[string]client.Client{},
			expectedCollectors: 0,
			validateAWS: func(t *testing.T, aws *AWS) {
				assert.Equal(t, 0, len(aws.collectors))
			},
		},
		{
			name:     "case insensitive service names",
			services: []string{"s3", "S3"},
			regions: []types.Region{
				{RegionName: stringPtr("us-east-1")},
			},
			setupMockClient: func(m *mock_client.MockClient) {
				m.EXPECT().DescribeRegions(gomock.Any(), false).Return(nil, nil).Times(2)
			},
			setupRegionClients: map[string]client.Client{},
			expectedCollectors: 2, // Both should create collectors
			validateAWS: func(t *testing.T, aws *AWS) {
				assert.Equal(t, 2, len(aws.collectors))
			},
		},
		{
			name:     "ELB service creates ELB collector",
			services: []string{"ELB"},
			regions: []types.Region{
				{RegionName: stringPtr("us-east-1")},
			},
			setupRegionClients: map[string]client.Client{
				"us-east-1": nil,
			},
			expectedCollectors: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			// Create mock client
			mockClient := mock_client.NewMockClient(ctrl)
			if tt.setupMockClient != nil {
				tt.setupMockClient(mockClient)
			}

			// Create mock region clients if needed
			regionClients := tt.setupRegionClients
			if regionClients == nil {
				regionClients = make(map[string]client.Client)
			}
			// Replace nil clients with mock clients
			for region := range regionClients {
				if regionClients[region] == nil {
					regionClients[region] = mock_client.NewMockClient(ctrl)
				}
			}

			// Create config
			config := &Config{
				Services:       tt.services,
				Region:         "us-east-1",
				ScrapeInterval: 60 * time.Second,
				Logger:         logger,
			}

			// Call function
			awsConfig := aws.Config{}
			aws, err := newWithDependencies(
				t.Context(),
				config,
				mockClient,
				regionClients,
				tt.regions,
				awsConfig,
			)

			// Validate results
			if tt.expectedError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, aws)
			assert.Equal(t, tt.expectedCollectors, len(aws.collectors), "unexpected number of collectors")
			assert.Equal(t, config, aws.Config)
			assert.NotNil(t, aws.logger)

			if tt.validateAWS != nil {
				tt.validateAWS(t, aws)
			}
		})
	}
}

// Helper function to create string pointers
func stringPtr(s string) *string {
	return &s
}

func Test_filterExcludedRegions(t *testing.T) {
	tests := []struct {
		name        string
		regions     []types.Region
		excludeList []string
		want        []types.Region
	}{
		{
			name: "empty exclude list returns regions unchanged",
			regions: []types.Region{
				{RegionName: stringPtr("us-east-1")},
				{RegionName: stringPtr("me-central-1")},
			},
			excludeList: nil,
			want: []types.Region{
				{RegionName: stringPtr("us-east-1")},
				{RegionName: stringPtr("me-central-1")},
			},
		},
		{
			name: "empty exclude list slice returns regions unchanged",
			regions: []types.Region{
				{RegionName: stringPtr("us-east-1")},
			},
			excludeList: []string{},
			want: []types.Region{
				{RegionName: stringPtr("us-east-1")},
			},
		},
		{
			name: "one excluded region is removed",
			regions: []types.Region{
				{RegionName: stringPtr("us-east-1")},
				{RegionName: stringPtr("me-central-1")},
				{RegionName: stringPtr("eu-west-1")},
			},
			excludeList: []string{"me-central-1"},
			want: []types.Region{
				{RegionName: stringPtr("us-east-1")},
				{RegionName: stringPtr("eu-west-1")},
			},
		},
		{
			name: "multiple excluded regions are removed",
			regions: []types.Region{
				{RegionName: stringPtr("us-east-1")},
				{RegionName: stringPtr("me-central-1")},
				{RegionName: stringPtr("me-south-1")},
				{RegionName: stringPtr("eu-west-1")},
			},
			excludeList: []string{"me-central-1", "me-south-1"},
			want: []types.Region{
				{RegionName: stringPtr("us-east-1")},
				{RegionName: stringPtr("eu-west-1")},
			},
		},
		{
			name: "exclude list entries are trimmed of whitespace",
			regions: []types.Region{
				{RegionName: stringPtr("us-east-1")},
				{RegionName: stringPtr("me-central-1")},
			},
			excludeList: []string{"  me-central-1  "},
			want: []types.Region{
				{RegionName: stringPtr("us-east-1")},
			},
		},
		{
			name: "nil RegionName is omitted when filtering",
			regions: []types.Region{
				{RegionName: stringPtr("us-east-1")},
				{RegionName: nil},
				{RegionName: stringPtr("eu-west-1")},
			},
			excludeList: []string{"other"}, // non-empty so we run the filter loop; nil is skipped
			want: []types.Region{
				{RegionName: stringPtr("us-east-1")},
				{RegionName: stringPtr("eu-west-1")},
			},
		},
		{
			name:        "empty regions returns empty",
			regions:     []types.Region{},
			excludeList: []string{"me-central-1"},
			want:        []types.Region{},
		},
		{
			name: "all regions excluded returns empty",
			regions: []types.Region{
				{RegionName: stringPtr("me-central-1")},
				{RegionName: stringPtr("me-south-1")},
			},
			excludeList: []string{"me-central-1", "me-south-1"},
			want:        []types.Region{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterExcludedRegions(tt.regions, tt.excludeList)
			require.Len(t, got, len(tt.want))
			for i := range tt.want {
				assert.Equal(t, tt.want[i].RegionName != nil, got[i].RegionName != nil)
				if tt.want[i].RegionName != nil && got[i].RegionName != nil {
					assert.Equal(t, *tt.want[i].RegionName, *got[i].RegionName)
				}
			}
		})
	}
}

func Test_RegisterCollectors(t *testing.T) {
	for _, tc := range []struct {
		name          string
		numCollectors int
		register      func(r provider.Registry) error
		expectedError error
	}{
		{
			name: "no error if no collectors",
		},
		{
			name:          "bubble-up single collector error",
			numCollectors: 1,
			register: func(r provider.Registry) error {
				return fmt.Errorf("test register error")
			},
			expectedError: fmt.Errorf("test register error"),
		},
		{
			name:          "two collectors with no errors",
			numCollectors: 2,
			register:      func(r provider.Registry) error { return nil },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			r := mock_provider.NewMockRegistry(ctrl)
			r.EXPECT().MustRegister(gomock.Any()).AnyTimes()
			c := mock_provider.NewMockCollector(ctrl)
			if tc.register != nil {
				c.EXPECT().Register(r).DoAndReturn(tc.register).Times(tc.numCollectors)
			}

			a := AWS{
				Config:           nil,
				collectors:       []provider.Collector{},
				logger:           logger,
				ctx:              t.Context(),
				collectorTimeout: 1 * time.Minute,
			}
			for i := 0; i < tc.numCollectors; i++ {
				a.collectors = append(a.collectors, c)
			}

			err := a.RegisterCollectors(r)
			if tc.expectedError != nil {
				require.EqualError(t, err, tc.expectedError.Error())
				return
			}
			require.NoError(t, err)
		})
	}
}

func Test_CollectConcurrencyLimit(t *testing.T) {
	// numCollectors is intentionally larger than collectConcurrencyLimit to
	// exercise the limit.  The mock Collect sleeps briefly so that goroutines
	// overlap in time; without SetLimit the peak would equal numCollectors.
	const numCollectors = 25

	var (
		mu                 sync.Mutex
		currentConcurrency int
		peakConcurrency    int
		totalCalls         int
	)

	ctrl := gomock.NewController(t)
	c := mock_provider.NewMockCollector(ctrl)
	c.EXPECT().Name().Return("concurrent-collector").AnyTimes()
	c.EXPECT().Register(gomock.Any()).Return(nil).AnyTimes()
	c.EXPECT().Collect(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, _ chan<- prometheus.Metric) error {
			mu.Lock()
			currentConcurrency++
			totalCalls++
			if currentConcurrency > peakConcurrency {
				peakConcurrency = currentConcurrency
			}
			mu.Unlock()

			time.Sleep(10 * time.Millisecond)

			mu.Lock()
			currentConcurrency--
			mu.Unlock()
			return nil
		},
	).Times(numCollectors)

	a := &AWS{
		Config:           nil,
		collectors:       make([]provider.Collector, numCollectors),
		logger:           logger,
		ctx:              t.Context(),
		collectorTimeout: 1 * time.Minute,
	}
	for i := range numCollectors {
		a.collectors[i] = c
	}

	// Use a buffered channel so goroutines can always send metrics without
	// blocking (gatherer internals emit a few metrics per collector).
	ch := make(chan prometheus.Metric, numCollectors*10)
	a.Collect(ch)
	close(ch)

	assert.Equal(t, numCollectors, totalCalls, "every collector must be called exactly once")
	assert.LessOrEqual(t, peakConcurrency, collectConcurrencyLimit,
		"peak concurrency must not exceed collectConcurrencyLimit")
}

func Test_CollectMetrics(t *testing.T) {
	tests := map[string]struct {
		numCollectors   int
		collectorName   string
		collect         func(context.Context, chan<- prometheus.Metric) error
		expectedMetrics []*utils.MetricResult
	}{
		"no error if no collectors": {
			numCollectors:   0,
			collectorName:   "test1",
			expectedMetrics: []*utils.MetricResult{},
		},
		"bubble-up single collector error": {
			numCollectors: 1,
			collectorName: "test2",
			collect: func(context.Context, chan<- prometheus.Metric) error {
				return fmt.Errorf("test collect error")
			},
			expectedMetrics: []*utils.MetricResult{
				{
					FqName:     "cloudcost_exporter_collector_last_scrape_error",
					Labels:     utils.LabelMap{"provider": "aws", "collector": "test2"},
					Value:      1,
					MetricType: prometheus.CounterValue,
				},
			},
		},
		"two collectors with no errors": {
			numCollectors: 2,
			collectorName: "test3",
			collect:       func(context.Context, chan<- prometheus.Metric) error { return nil },
			expectedMetrics: []*utils.MetricResult{
				{
					FqName:     "cloudcost_exporter_collector_last_scrape_error",
					Labels:     utils.LabelMap{"provider": "aws", "collector": "test3"},
					Value:      0,
					MetricType: prometheus.CounterValue,
				},
				{
					FqName:     "cloudcost_exporter_collector_last_scrape_error",
					Labels:     utils.LabelMap{"provider": "aws", "collector": "test3"},
					Value:      0,
					MetricType: prometheus.CounterValue,
				},
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ch := make(chan prometheus.Metric)

			ctrl := gomock.NewController(t)
			c := mock_provider.NewMockCollector(ctrl)
			registry := mock_provider.NewMockRegistry(ctrl)
			registry.EXPECT().MustRegister(gomock.Any()).AnyTimes()
			if tt.collect != nil {
				c.EXPECT().Name().Return(tt.collectorName).AnyTimes()
				c.EXPECT().Collect(gomock.Any(), ch).DoAndReturn(tt.collect).AnyTimes()
				c.EXPECT().Register(gomock.Any()).Return(nil).AnyTimes()
				c.EXPECT().Describe(gomock.Any()).Return(nil).AnyTimes()
			}
			aws := &AWS{
				Config:           &Config{Region: "us-east-1"},
				collectors:       []provider.Collector{},
				logger:           logger,
				ctx:              t.Context(),
				collectorTimeout: 1 * time.Minute,
			}

			for range tt.numCollectors {
				aws.collectors = append(aws.collectors, c)
			}

			wg := sync.WaitGroup{}

			wg.Add(1)
			go func() {
				aws.Collect(ch)
				close(ch)
			}()
			wg.Done()

			wg.Wait()
			var metrics []*utils.MetricResult
			var ignoreMetric = func(metricName string) bool {
				ignoredMetricSuffix := []string{
					"duration_seconds",
					"last_scrape_time",
					"collector_total",
				}
				for _, suffix := range ignoredMetricSuffix {
					if strings.Contains(metricName, suffix) {
						return true
					}
				}

				return false
			}
			for m := range ch {
				metric := utils.ReadMetrics(m)
				if metric == nil { // ReadMetrics can't parse histograms
					continue
				}
				if ignoreMetric(metric.FqName) {
					continue
				}
				metrics = append(metrics, metric)
			}
			assert.ElementsMatch(t, metrics, tt.expectedMetrics)
		})
	}
}
