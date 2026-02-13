package gatherer

import (
	"context"
	"log/slog"
	"time"

	cloudcost_exporter "github.com/grafana/cloudcost-exporter"
	"github.com/grafana/cloudcost-exporter/pkg/provider"
	"github.com/prometheus/client_golang/prometheus"
	io_prometheus_client "github.com/prometheus/client_model/go"
)

var gathererDurationHistogramVec = prometheus.NewHistogramVec(
	prometheus.HistogramOpts{
		Name:                           prometheus.BuildFQName(cloudcost_exporter.ExporterName, "collector", "duration_seconds"),
		Help:                           "Duration of a collector scrape in seconds",
		NativeHistogramBucketFactor:    1.1,
		NativeHistogramMaxBucketNumber: 100,
	},
	[]string{"collector"},
)

var gathererErrorCounterVec = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: prometheus.BuildFQName(cloudcost_exporter.ExporterName, "collector", "error_total"),
		Help: "Total number of errors that occurred during the last scrape.",
	},
	[]string{"collector"},
)

var gathererTotalCounterVec = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: prometheus.BuildFQName(cloudcost_exporter.ExporterName, "collector", "total"),
		Help: "Total number of scrapes.",
	},
	[]string{"collector"},
)

func emitHistogramMetric(ch chan<- prometheus.Metric, collectorName string, duration float64) {
	ch <- prometheus.MustNewConstHistogram(
		gathererDurationHistogramVec.WithLabelValues(collectorName).(prometheus.Histogram).Desc(),
		1,
		duration,
		nil,
		collectorName,
	)

	counter := gathererTotalCounterVec.WithLabelValues(collectorName)
	counter.Inc()

	m := &io_prometheus_client.Metric{}
	if err := counter.Write(m); err == nil && m.Counter != nil {
		ch <- prometheus.MustNewConstMetric(
			gathererTotalCounterVec.WithLabelValues(collectorName).Desc(),
			prometheus.CounterValue,
			m.GetCounter().GetValue(),
			collectorName,
		)
	}
}

// CollectWithGatherer collects metrics from a collector and uses the Gatherer interface to detect errors.
func CollectWithGatherer(ctx context.Context, c provider.Collector, ch chan<- prometheus.Metric, logger *slog.Logger) (float64, bool) {
	start := time.Now()
	var hasError bool
	var duration float64

	tempRegistry := prometheus.NewRegistry()
	// also register errors if the temporary registry to detect errors via Gatherer interface fails
	if err := c.Register(tempRegistry); err != nil {
		hasError = true
		logger.LogAttrs(ctx, slog.LevelError, "could not register collector with gatherer",
			slog.String("collector", c.Name()),
			slog.String("message", err.Error()),
		)
	}

	collectErr := c.Collect(ctx, ch)
	duration = time.Since(start).Seconds()
	if collectErr != nil {
		hasError = true
		logger.LogAttrs(ctx, slog.LevelError, "could not collect metrics",
			slog.String("collector", c.Name()),
			slog.String("message", collectErr.Error()),
		)
	}

	if _, err := tempRegistry.Gather(); err != nil {
		hasError = true
		logger.LogAttrs(ctx, slog.LevelError, "did not detect gatherer",
			slog.String("collector", c.Name()),
			slog.String("message", err.Error()),
		)
		errorCounter := gathererErrorCounterVec.WithLabelValues(c.Name())
		errorCounter.Inc()

		m := &io_prometheus_client.Metric{}
		if err := errorCounter.Write(m); err == nil && m.Counter != nil {
			ch <- prometheus.MustNewConstMetric(
				gathererErrorCounterVec.WithLabelValues(c.Name()).Desc(),
				prometheus.CounterValue,
				m.GetCounter().GetValue(),
				c.Name(),
			)
		}
	}

	emitHistogramMetric(ch, c.Name(), duration)

	return duration, hasError
}
