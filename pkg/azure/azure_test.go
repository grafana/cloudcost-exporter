package azure

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/grafana/cloudcost-exporter/pkg/provider"
	mock_provider "github.com/grafana/cloudcost-exporter/pkg/provider/mocks"
	"github.com/grafana/cloudcost-exporter/pkg/utils"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

var (
	testLogger = slog.New(slog.NewTextHandler(os.Stdout, nil))
)

// recordingHandler captures emitted log records so a test can assert on them.
type recordingHandler struct {
	mu      *sync.Mutex
	records *[]slog.Record
}

func (h recordingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h recordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	*h.records = append(*h.records, r)
	return nil
}
func (h recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h recordingHandler) WithGroup(string) slog.Handler      { return h }

// Test_New_experimentalService proves the experimental path end to end for Azure: New reads
// ExperimentalServices, routes it through provider.MergeServiceEntries, registers the collector,
// and emits the shared experimental warning.
func Test_New_experimentalService(t *testing.T) {
	var mu sync.Mutex
	var records []slog.Record
	logger := slog.New(recordingHandler{mu: &mu, records: &records})

	a, err := New(t.Context(), &Config{
		Logger:               logger,
		SubscriptionID:       "asdf-1234",
		ExperimentalServices: []string{"blob"},
	})
	assert.NoError(t, err)
	assert.NotNil(t, a)
	assert.Len(t, a.collectors, 1, "experimental blob service should register a collector")

	mu.Lock()
	defer mu.Unlock()
	warned := false
	for _, r := range records {
		if r.Level == slog.LevelWarn && strings.Contains(r.Message, "experimental collector") {
			warned = true
		}
	}
	assert.True(t, warned, "expected an experimental-collector warning to be logged")
}

func Test_New(t *testing.T) {
	testTable := map[string]struct {
		expectedErr error
		subId       string
	}{
		"no subscription ID": {
			expectedErr: errInvalidSubscriptionID,
			subId:       "",
		},

		"base case": {
			expectedErr: nil,
			subId:       "asdf-1234",
		},
	}

	for name, tc := range testTable {
		t.Run(name, func(t *testing.T) {
			a, err := New(t.Context(), &Config{
				Logger:         testLogger,
				SubscriptionID: tc.subId,
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
				context: t.Context(),
			}
			for _, c := range tc.mockCollectors {
				call := c.EXPECT().Register(gomock.Any()).AnyTimes()
				call.Return(nil)

				azProvider.collectors = append(azProvider.collectors, c)
			}

			err := azProvider.RegisterCollectors(mockRegistry)
			assert.Equal(t, err, tc.expectedErr)
		})
	}
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
					Labels:     utils.LabelMap{"provider": "azure", "collector": "test2"},
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
					Labels:     utils.LabelMap{"provider": "azure", "collector": "test3"},
					Value:      0,
					MetricType: prometheus.CounterValue,
				},
				{
					FqName:     "cloudcost_exporter_collector_last_scrape_error",
					Labels:     utils.LabelMap{"provider": "azure", "collector": "test3"},
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
			azure := &Azure{
				context:          t.Context(),
				logger:           testLogger,
				collectors:       []provider.Collector{},
				collectorTimeout: 1 * time.Minute,
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
					"collector_total",
					"collector_error",
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

func TestServices(t *testing.T) {
	got := Services()
	wantNames := []string{serviceAKS, serviceBlob, serviceEventHubs}
	gotNames := make([]string, 0, len(got))
	for _, s := range got {
		gotNames = append(gotNames, s.Name)
		assert.NotEmpty(t, s.DisplayName, "DisplayName empty for %s", s.Name)
		assert.NotEmpty(t, s.Description, "Description empty for %s", s.Name)
	}
	assert.ElementsMatch(t, wantNames, gotNames)

	var eh provider.ServiceInfo
	for _, s := range got {
		if s.Name == serviceEventHubs {
			eh = s
			break
		}
	}
	assert.Contains(t, eh.Aliases, serviceEventHubsAlias, "EVENTHUBS should carry EVENTHUB as alias")
}
