package collectormetrics

import (
	"context"
	"log/slog"
	"testing"

	mock_provider "github.com/grafana/cloudcost-exporter/pkg/provider/mocks"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestCollect(t *testing.T) {
	tests := map[string]struct {
		collectorName    string
		collect          func(context.Context, chan<- prometheus.Metric) error
		expectedHasError bool
	}{
		"no error when collect succeeds": {
			collectorName: "collector_1",
			collect:       func(context.Context, chan<- prometheus.Metric) error { return nil },
		},
		"error when collect fails": {
			collectorName:    "collector_2",
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
			c.EXPECT().Name().Return(tt.collectorName).AnyTimes()
			if tt.collect != nil {
				c.EXPECT().Collect(gomock.Any(), ch).DoAndReturn(tt.collect).AnyTimes()
			}

			duration, hasError := Collect(context.Background(), c, ch, slog.Default())

			close(ch)

			assert.GreaterOrEqual(t, duration, 0.0)
			assert.Equal(t, tt.expectedHasError, hasError)
		})
	}
}

// TestCollect_ErrorCounterEmitted proves that the error counter metric
// is emitted to the channel when a collector's Collect method returns an error.
func TestCollect_ErrorCounterEmitted(t *testing.T) {
	const collectorName = "error_counter_proof"
	ch := make(chan prometheus.Metric, 20)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	c := mock_provider.NewMockCollector(ctrl)
	c.EXPECT().Name().Return(collectorName).AnyTimes()
	c.EXPECT().Collect(gomock.Any(), ch).Return(assert.AnError)

	_, hasError := Collect(context.Background(), c, ch, slog.Default())
	close(ch)

	assert.True(t, hasError, "hasError should be true when Collect fails")

	descCh := make(chan *prometheus.Desc, 1)
	errorCounterVec.Describe(descCh)
	expectedDesc := <-descCh

	// Drain the full channel and verify the error counter was emitted.
	var found bool
	var counterValue float64
	for m := range ch {
		if m.Desc() != expectedDesc {
			continue
		}
		var dtoMetric dto.Metric
		require.NoError(t, m.Write(&dtoMetric))
		if labels := dtoMetric.GetLabel(); len(labels) == 1 && labels[0].GetValue() == collectorName {
			counterValue = dtoMetric.GetCounter().GetValue()
			found = true
		}
	}
	assert.True(t, found, "error counter metric should be emitted to channel when Collect fails")
	assert.Equal(t, 1.0, counterValue, "error counter should be incremented by 1")
}

type collectorConfig struct {
	name    string
	collect func(context.Context, chan<- prometheus.Metric) error
}

func TestCollect_MultipleCollectors(t *testing.T) {
	tests := map[string]struct {
		collectors     []collectorConfig
		expectedErrors []bool
	}{
		"multiple collectors all succeed": {
			collectors: []collectorConfig{
				{name: "collector_1", collect: func(context.Context, chan<- prometheus.Metric) error { return nil }},
				{name: "collector_2", collect: func(context.Context, chan<- prometheus.Metric) error { return nil }},
				{name: "collector_3", collect: func(context.Context, chan<- prometheus.Metric) error { return nil }},
			},
			expectedErrors: []bool{false, false, false},
		},
		"multiple collectors some fail": {
			collectors: []collectorConfig{
				{name: "collector_1", collect: func(context.Context, chan<- prometheus.Metric) error { return nil }},
				{name: "collector_2", collect: func(context.Context, chan<- prometheus.Metric) error { return assert.AnError }},
				{name: "collector_3", collect: func(context.Context, chan<- prometheus.Metric) error { return nil }},
			},
			expectedErrors: []bool{false, true, false},
		},
		"multiple collectors all fail": {
			collectors: []collectorConfig{
				{name: "collector_1", collect: func(context.Context, chan<- prometheus.Metric) error { return assert.AnError }},
				{name: "collector_2", collect: func(context.Context, chan<- prometheus.Metric) error { return assert.AnError }},
				{name: "collector_3", collect: func(context.Context, chan<- prometheus.Metric) error { return assert.AnError }},
			},
			expectedErrors: []bool{true, true, true},
		},
		"multiple collectors with mixed failures": {
			collectors: []collectorConfig{
				{name: "collector_1", collect: func(context.Context, chan<- prometheus.Metric) error { return assert.AnError }},
				{name: "collector_2", collect: func(context.Context, chan<- prometheus.Metric) error { return nil }},
				{name: "collector_3", collect: func(context.Context, chan<- prometheus.Metric) error { return nil }},
				{name: "collector_4", collect: func(context.Context, chan<- prometheus.Metric) error { return assert.AnError }},
			},
			expectedErrors: []bool{true, false, false, true},
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
				c.EXPECT().Name().Return(col.name).AnyTimes()
				if col.collect != nil {
					c.EXPECT().Collect(gomock.Any(), ch).DoAndReturn(col.collect).AnyTimes()
				}
				collectors = append(collectors, c)
			}

			for i, c := range collectors {
				duration, hasError := Collect(context.Background(), c, ch, slog.Default())
				assert.GreaterOrEqual(t, duration, 0.0, "collector %d duration should be >= 0", i)
				assert.Equal(t, tt.expectedErrors[i], hasError, "collector %d error status mismatch", i)
			}

			close(ch)
		})
	}
}
