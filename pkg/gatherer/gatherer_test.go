package gatherer

import (
	"context"
	"log/slog"
	"testing"

	mock_provider "github.com/grafana/cloudcost-exporter/pkg/provider/mocks"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

func TestCollectWithGatherer(t *testing.T) {
	tests := map[string]struct {
		collectorName    string
		registerErr      error
		collect          func(context.Context, chan<- prometheus.Metric) error
		expectedHasError bool
	}{
		"no error when collect succeeds": {
			collectorName: "collector_1",
			registerErr:   nil,
			collect:       func(context.Context, chan<- prometheus.Metric) error { return nil },
		},
		"error when collect fails": {
			collectorName:    "collector_2",
			registerErr:      nil,
			collect:          func(context.Context, chan<- prometheus.Metric) error { return assert.AnError },
			expectedHasError: true,
		},
		"error when register fails": {
			collectorName:    "collector_3",
			registerErr:      assert.AnError,
			collect:          func(context.Context, chan<- prometheus.Metric) error { return nil },
			expectedHasError: true,
		},
		"error when both register and collect fail": {
			collectorName:    "collector_4",
			registerErr:      assert.AnError,
			collect:          func(context.Context, chan<- prometheus.Metric) error { return assert.AnError },
			expectedHasError: true,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ch := make(chan prometheus.Metric, 10) // Buffered channel to prevent blocking
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			c := mock_provider.NewMockCollector(ctrl)
			c.EXPECT().Register(gomock.Any()).Return(tt.registerErr).AnyTimes()
			c.EXPECT().Name().Return(tt.collectorName).AnyTimes()
			if tt.collect != nil {
				c.EXPECT().Collect(gomock.Any(), ch).DoAndReturn(tt.collect).AnyTimes()
			}
			c.EXPECT().Describe(gomock.Any()).Return(nil).AnyTimes()

			duration, hasError := CollectWithGatherer(context.Background(), c, ch, slog.Default())

			close(ch)

			assert.GreaterOrEqual(t, duration, 0.0)
			assert.Equal(t, tt.expectedHasError, hasError)
		})
	}
}

type collectorConfig struct {
	name        string
	registerErr error
	collect     func(context.Context, chan<- prometheus.Metric) error
}

func TestCollectWithGatherer_MultipleCollectors(t *testing.T) {
	tests := map[string]struct {
		collectors     []collectorConfig
		expectedErrors []bool
	}{
		"multiple collectors all succeed": {
			collectors: []collectorConfig{
				{
					name:        "collector_1",
					registerErr: nil,
					collect:     func(context.Context, chan<- prometheus.Metric) error { return nil },
				},
				{
					name:        "collector_2",
					registerErr: nil,
					collect:     func(context.Context, chan<- prometheus.Metric) error { return nil },
				},
				{
					name:        "collector_3",
					registerErr: nil,
					collect:     func(context.Context, chan<- prometheus.Metric) error { return nil },
				},
			},
			expectedErrors: []bool{false, false, false},
		},
		"multiple collectors some fail": {
			collectors: []collectorConfig{
				{
					name:        "collector_1",
					registerErr: nil,
					collect:     func(context.Context, chan<- prometheus.Metric) error { return nil },
				},
				{
					name:        "collector_2",
					registerErr: nil,
					collect:     func(context.Context, chan<- prometheus.Metric) error { return assert.AnError },
				},
				{
					name:        "collector_3",
					registerErr: nil,
					collect:     func(context.Context, chan<- prometheus.Metric) error { return nil },
				},
			},
			expectedErrors: []bool{false, true, false},
		},
		"multiple collectors all fail": {
			collectors: []collectorConfig{
				{
					name:        "collector_1",
					registerErr: nil,
					collect:     func(context.Context, chan<- prometheus.Metric) error { return assert.AnError },
				},
				{
					name:        "collector_2",
					registerErr: nil,
					collect:     func(context.Context, chan<- prometheus.Metric) error { return assert.AnError },
				},
				{
					name:        "collector_3",
					registerErr: nil,
					collect:     func(context.Context, chan<- prometheus.Metric) error { return assert.AnError },
				},
			},
			expectedErrors: []bool{true, true, true},
		},
		"multiple collectors with register failures": {
			collectors: []collectorConfig{
				{
					name:        "collector_1",
					registerErr: nil,
					collect:     func(context.Context, chan<- prometheus.Metric) error { return nil },
				},
				{
					name:        "collector_2",
					registerErr: assert.AnError,
					collect:     func(context.Context, chan<- prometheus.Metric) error { return nil },
				},
				{
					name:        "collector_3",
					registerErr: nil,
					collect:     func(context.Context, chan<- prometheus.Metric) error { return assert.AnError },
				},
			},
			expectedErrors: []bool{false, true, true},
		},
		"multiple collectors with mixed failures": {
			collectors: []collectorConfig{
				{
					name:        "collector_1",
					registerErr: assert.AnError,
					collect:     func(context.Context, chan<- prometheus.Metric) error { return assert.AnError },
				},
				{
					name:        "collector_2",
					registerErr: nil,
					collect:     func(context.Context, chan<- prometheus.Metric) error { return nil },
				},
				{
					name:        "collector_3",
					registerErr: assert.AnError,
					collect:     func(context.Context, chan<- prometheus.Metric) error { return nil },
				},
				{
					name:        "collector_4",
					registerErr: nil,
					collect:     func(context.Context, chan<- prometheus.Metric) error { return assert.AnError },
				},
			},
			expectedErrors: []bool{true, false, true, true},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ch := make(chan prometheus.Metric, 100) // Large buffer for multiple collectors
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			var collectors []*mock_provider.MockCollector
			for _, col := range tt.collectors {
				c := mock_provider.NewMockCollector(ctrl)
				c.EXPECT().Register(gomock.Any()).Return(col.registerErr).AnyTimes()
				c.EXPECT().Name().Return(col.name).AnyTimes()
				if col.collect != nil {
					c.EXPECT().Collect(gomock.Any(), ch).DoAndReturn(col.collect).AnyTimes()
				}
				c.EXPECT().Describe(gomock.Any()).Return(nil).AnyTimes()
				collectors = append(collectors, c)
			}

			var durations []float64
			var hasErrors []bool
			for i, c := range collectors {
				duration, hasError := CollectWithGatherer(context.Background(), c, ch, slog.Default())
				durations = append(durations, duration)
				hasErrors = append(hasErrors, hasError)
				assert.GreaterOrEqual(t, duration, 0.0, "collector %d duration should be >= 0", i)
				assert.Equal(t, tt.expectedErrors[i], hasError, "collector %d error status mismatch", i)
			}

			close(ch)
		})
	}
}
