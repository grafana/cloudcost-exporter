package azure

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/grafana/cloudcost-exporter/pkg/provider"
	mock_provider "github.com/grafana/cloudcost-exporter/pkg/provider/mocks"
	"github.com/grafana/cloudcost-exporter/pkg/utils"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

var (
	parentCtx  context.Context = context.TODO()
	testLogger *slog.Logger    = slog.New(slog.NewTextHandler(os.Stdout, nil))
)

func Test_New(t *testing.T) {
	testTable := map[string]struct {
		expectedErr error
		subId       string
	}{
		"no subscription ID": {
			expectedErr: InvalidSubscriptionId,
			subId:       "",
		},

		"base case": {
			expectedErr: nil,
			subId:       "asdf-1234",
		},
	}

	for name, tc := range testTable {
		t.Run(name, func(t *testing.T) {
			a, err := New(parentCtx, &Config{
				Logger:         testLogger,
				SubscriptionId: tc.subId,
			})
			if tc.expectedErr != nil {
				assert.ErrorIs(t, err, tc.expectedErr)
				return
			}
			assert.NoError(t, err)
			assert.NotNil(t, a)
		})
	}
}

func Test_RegisterCollectors(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockRegistry := mock_provider.NewMockRegistry(ctrl)

	testCases := map[string]struct {
		mockCollectors []*mock_provider.MockCollector
		expectedErr    error
	}{
		"no collectors": {
			mockCollectors: []*mock_provider.MockCollector{},
		},
		"AKS collector": {
			mockCollectors: []*mock_provider.MockCollector{
				mock_provider.NewMockCollector(ctrl),
			},
		},
		"AKS and future storage collector": {
			mockCollectors: []*mock_provider.MockCollector{
				mock_provider.NewMockCollector(ctrl),
				mock_provider.NewMockCollector(ctrl),
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			azProvider := &Azure{
				logger:  testLogger,
				context: parentCtx,
			}
			for _, c := range tc.mockCollectors {
				call := c.EXPECT().Register(gomock.Any()).AnyTimes()
				call.Return(nil)

				azProvider.collectors = append(azProvider.collectors, c)
			}

			mockRegistry.EXPECT().MustRegister(gomock.Any()).Times(1)
			err := azProvider.RegisterCollectors(mockRegistry)
			assert.Equal(t, err, tc.expectedErr)
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
					FqName:     "cloudcost_exporter_collector_success",
					Labels:     utils.LabelMap{"provider": "azure", "collector": "test2"},
					Value:      0,
					MetricType: prometheus.GaugeValue,
				},
				{
					FqName:     "cloudcost_exporter_collector_last_scrape_error",
					Labels:     utils.LabelMap{"provider": "azure", "collector": "test2"},
					Value:      1,
					MetricType: prometheus.GaugeValue,
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
					Labels:     utils.LabelMap{"provider": "azure", "collector": "test3"},
					Value:      0,
					MetricType: prometheus.GaugeValue,
				},
				{
					FqName:     "cloudcost_exporter_collector_last_scrape_error",
					Labels:     utils.LabelMap{"provider": "azure", "collector": "test3"},
					Value:      0,
					MetricType: prometheus.GaugeValue,
				},
				{
					FqName:     "cloudcost_exporter_collector_success",
					Labels:     utils.LabelMap{"provider": "azure", "collector": "test3"},
					Value:      1,
					MetricType: prometheus.GaugeValue,
				},
				{
					FqName:     "cloudcost_exporter_collector_success",
					Labels:     utils.LabelMap{"provider": "azure", "collector": "test3"},
					Value:      2,
					MetricType: prometheus.GaugeValue,
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
				c.EXPECT().Collect(ch).DoAndReturn(tt.collect).AnyTimes()
				c.EXPECT().Register(registry).Return(nil).AnyTimes()
			}
			azure := &Azure{
				context:    parentCtx,
				logger:     testLogger,
				collectors: []provider.Collector{},
			}

			for range tt.numCollectors {
				azure.collectors = append(azure.collectors, c)
			}

			wg := sync.WaitGroup{}

			wg.Add(1)
			go func() {
				azure.Collect(ch)
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
			// clean up metrics for next test
			metrics = []*utils.MetricResult{}
			azure.collectors = []provider.Collector{}
		})
	}
}
