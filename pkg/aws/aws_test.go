package aws

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/grafana/cloudcost-exporter/pkg/provider"
	"github.com/grafana/cloudcost-exporter/pkg/utils"

	mock_client "github.com/grafana/cloudcost-exporter/pkg/aws/client/mocks"
	mock_provider "github.com/grafana/cloudcost-exporter/pkg/provider/mocks"
)

var logger = slog.New(slog.NewTextHandler(os.Stdout, nil))

func Test_New(t *testing.T) {
	for _, tc := range []struct {
		name          string
		expectedError error
		config        *Config
	}{
		{
			name:          "no error",
			expectedError: nil,
			config: &Config{
				Logger: logger,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			r := mock_client.NewMockClient(ctrl)

			a, err := New(context.Background(), tc.config)
			if tc.expectedError != nil {
				require.EqualError(t, err, tc.expectedError.Error())
				return
			}
			require.NoError(t, err)
			require.NotNil(t, a)
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
				Config:     nil,
				collectors: []provider.Collector{},
				logger:     logger,
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

func Test_CollectMetrics(t *testing.T) {
	tests := map[string]struct {
		numCollectors   int
		collectorName   string
		collect         func(chan<- prometheus.Metric) error
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
			collect: func(chan<- prometheus.Metric) error {
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
			collect:       func(chan<- prometheus.Metric) error { return nil },
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
			if tt.collect != nil {
				c.EXPECT().Name().Return(tt.collectorName).AnyTimes()
				c.EXPECT().Collect(ch).DoAndReturn(tt.collect).AnyTimes()
				c.EXPECT().Register(registry).Return(nil).AnyTimes()
			}
			aws := &AWS{
				Config:     nil,
				collectors: []provider.Collector{},
				logger:     logger,
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
				if ignoreMetric(metric.FqName) {
					continue
				}
				metrics = append(metrics, metric)
			}
			assert.ElementsMatch(t, metrics, tt.expectedMetrics)
		})
	}
}
