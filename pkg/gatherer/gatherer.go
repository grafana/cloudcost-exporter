package gatherer

import (
	"context"
	"log/slog"
	"time"

	cloudcost_exporter "github.com/grafana/cloudcost-exporter"
	"github.com/grafana/cloudcost-exporter/pkg/provider"
	"github.com/prometheus/client_golang/prometheus"
)

var gathererDurationHistogramVec = prometheus.NewHistogramVec(
	prometheus.HistogramOpts{
		Name:                           prometheus.BuildFQName(cloudcost_exporter.ExporterName, "collector", "duration_seconds"),
		Help:                           "Duration of a collector scrape in seconds",
		NativeHistogramBucketFactor:    1.1,
		NativeHistogramMaxBucketNumber: 100,
	},
	[]string{"collector", "region"},
)

var gathererErrorCounterVec = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: prometheus.BuildFQName(cloudcost_exporter.ExporterName, "collector", "error_total"),
		Help: "Total number of errors that occurred during the last scrape.",
	},
	[]string{"collector", "region"},
)

var gathererTotalCounterVec = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: prometheus.BuildFQName(cloudcost_exporter.ExporterName, "collector", "total"),
		Help: "Total number of scrapes.",
	},
	[]string{"collector", "region"},
)

func emitHistogramMetric(ch chan<- prometheus.Metric, collectorName string, region string, duration float64) {
	h := gathererDurationHistogramVec.WithLabelValues(collectorName, region).(prometheus.Histogram)
	h.Observe(duration)
	ch <- h

	counter := gathererTotalCounterVec.WithLabelValues(collectorName, region)
	counter.Inc()
	ch <- counter
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

	regions := []string{"unknown"}
	if rp, ok := c.(provider.RegionsProvider); ok {
		if r := rp.Regions(); len(r) > 0 {
			regions = r
		}
	}

	if _, err := tempRegistry.Gather(); err != nil {
		hasError = true
		logger.LogAttrs(ctx, slog.LevelError, "did not detect gatherer",
			slog.String("collector", c.Name()),
			slog.String("message", err.Error()),
		)
		errorCounter := gathererErrorCounterVec.WithLabelValues(c.Name())
		errorCounter.Inc()
		ch <- errorCounter
	}

	for _, region := range regions {
		emitHistogramMetric(ch, c.Name(), region, duration)
	}

	return duration, hasError
}
