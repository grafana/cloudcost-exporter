package collectormetrics

import (
	"context"
	"log/slog"
	"time"

	cloudcost_exporter "github.com/grafana/cloudcost-exporter"
	"github.com/grafana/cloudcost-exporter/pkg/provider"
	"github.com/prometheus/client_golang/prometheus"
)

var durationHistogramVec = prometheus.NewHistogramVec(
	prometheus.HistogramOpts{
		Name:                           prometheus.BuildFQName(cloudcost_exporter.ExporterName, "collector", "duration_seconds"),
		Help:                           "Duration of a collector scrape in seconds",
		NativeHistogramBucketFactor:    1.1,
		NativeHistogramMaxBucketNumber: 100,
	},
	[]string{"collector"},
)

var errorCounterVec = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: prometheus.BuildFQName(cloudcost_exporter.ExporterName, "collector", "error_total"),
		Help: "Total number of errors that occurred during the last scrape.",
	},
	[]string{"collector"},
)

var totalCounterVec = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: prometheus.BuildFQName(cloudcost_exporter.ExporterName, "collector", "total"),
		Help: "Total number of scrapes.",
	},
	[]string{"collector"},
)

func emitOperationalMetrics(ch chan<- prometheus.Metric, collectorName string, duration float64) {
	h := durationHistogramVec.WithLabelValues(collectorName).(prometheus.Histogram)
	h.Observe(duration)
	ch <- h

	counter := totalCounterVec.WithLabelValues(collectorName)
	counter.Inc()
	ch <- counter
}

// Collect collects metrics from a collector and emits operational metrics to the channel.
func Collect(ctx context.Context, c provider.Collector, ch chan<- prometheus.Metric, logger *slog.Logger) (float64, bool) {
	start := time.Now()
	var hasError bool
	var duration float64

	collectErr := c.Collect(ctx, ch)
	duration = time.Since(start).Seconds()
	if collectErr != nil {
		hasError = true
		logger.LogAttrs(ctx, slog.LevelError, "could not collect metrics",
			slog.String("collector", c.Name()),
			slog.String("message", collectErr.Error()),
		)
		errorCounter := errorCounterVec.WithLabelValues(c.Name())
		errorCounter.Inc()
		ch <- errorCounter
	}

	emitOperationalMetrics(ch, c.Name(), duration)

	return duration, hasError
}
