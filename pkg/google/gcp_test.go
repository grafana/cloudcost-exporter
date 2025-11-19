package google

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/grafana/cloudcost-exporter/pkg/provider"
	mock_provider "github.com/grafana/cloudcost-exporter/pkg/provider/mocks"
	"github.com/grafana/cloudcost-exporter/pkg/utils"
)

var logger = slog.New(slog.NewTextHandler(os.Stdout, nil))

func Test_RegisterCollectors(t *testing.T) {
	tests := map[string]struct {
		numCollectors int
		register      func(r provider.Registry) error
		expectedError error
	}{
		"no error if no collectors": {},
		"bubble-up single collector error": {
			numCollectors: 1,
			register: func(r provider.Registry) error {
				return fmt.Errorf("test register error")
			},
			expectedError: fmt.Errorf("test register error"),
		},
		"two collectors with no errors": {
			numCollectors: 2,
			register:      func(r provider.Registry) error { return nil },
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			r := mock_provider.NewMockRegistry(ctrl)
			r.EXPECT().MustRegister(gomock.Any()).AnyTimes()

			c := mock_provider.NewMockCollector(ctrl)
			if tt.register != nil {
				c.EXPECT().Register(r).DoAndReturn(tt.register).Times(tt.numCollectors)
			}
			gcp := &GCP{
				config:           &Config{},
				collectors:       []provider.Collector{},
				logger:           logger,
				ctx:              t.Context(),
				collectorTimeout: 1 * time.Minute,
			}
			for range tt.numCollectors {
				gcp.collectors = append(gcp.collectors, c)
			}
			err := gcp.RegisterCollectors(r)
			if tt.expectedError != nil {
				require.EqualError(t, err, tt.expectedError.Error())
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestGCP_CollectMetrics(t *testing.T) {
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
					Labels:     utils.LabelMap{"provider": "gcp", "collector": "test2"},
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
					Labels:     utils.LabelMap{"provider": "gcp", "collector": "test3"},
					Value:      0,
					MetricType: prometheus.CounterValue,
				},
				{
					FqName:     "cloudcost_exporter_collector_last_scrape_error",
					Labels:     utils.LabelMap{"provider": "gcp", "collector": "test3"},
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
				// TODO: @pokom need to figure out why _sometimes_ this fails if we set it to *.Times(tt.numCollectors)
				c.EXPECT().Collect(gomock.Any(), ch).DoAndReturn(tt.collect).AnyTimes()
				c.EXPECT().Register(registry).Return(nil).AnyTimes()
			}
			gcp := &GCP{
				config:           &Config{},
				collectors:       []provider.Collector{},
				logger:           logger,
				ctx:              t.Context(),
				collectorTimeout: 1 * time.Minute,
			}

			for range tt.numCollectors {
				gcp.collectors = append(gcp.collectors, c)
			}

			wg := sync.WaitGroup{}

			wg.Add(1)
			go func() {
				gcp.Collect(ch)
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
