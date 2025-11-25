package gatherer

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	cloudcost_exporter "github.com/grafana/cloudcost-exporter"
	"github.com/grafana/cloudcost-exporter/pkg/provider"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	gathererDurationHistogramVec = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:                           prometheus.BuildFQName(cloudcost_exporter.ExporterName, "collector", "duration_seconds"),
			Help:                           "Duration of a collector scrape in seconds with error status.",
			NativeHistogramBucketFactor:    1.1,
			NativeHistogramMaxBucketNumber: 100,
		},
		[]string{"collector", "is_error"},
	)
)

// CollectWithGatherer collects metrics from a collector and uses the Gatherer interface to detect errors.
func CollectWithGatherer(ctx context.Context, c provider.Collector, ch chan<- prometheus.Metric, logger *slog.Logger) (float64, bool) {
	start := time.Now()
	var hasError bool
	var duration float64

	tempRegistry := prometheus.NewRegistry()
	// also register errors if the remporary registry to detect errors via Gatherer interface fails
	if err := c.Register(tempRegistry); err != nil {
		logger.LogAttrs(ctx, slog.LevelError, "could not register collector with gatherer",
			slog.String("collector", c.Name()),
			slog.String("message", err.Error()),
		)
		hasError = true
		duration = time.Since(start).Seconds()
		ch <- prometheus.MustNewConstHistogram(
			gathererDurationHistogramVec.WithLabelValues(c.Name(), strconv.FormatBool(hasError)).(prometheus.Histogram).Desc(),
			1,
			duration,
			nil,
			c.Name(),
			strconv.FormatBool(hasError),
		)
		return duration, hasError
	}

	collectErr := c.Collect(ctx, ch)
	if collectErr != nil {
		logger.LogAttrs(ctx, slog.LevelError, "could not collect metrics",
			slog.String("collector", c.Name()),
			slog.String("message", collectErr.Error()),
		)
	}

	if _, err := tempRegistry.Gather(); err != nil {
		logger.LogAttrs(ctx, slog.LevelError, "did not detect gatherer",
			slog.String("collector", c.Name()),
			slog.String("message", err.Error()),
		)
		hasError = true
		duration = time.Since(start).Seconds()
		ch <- prometheus.MustNewConstHistogram(
			gathererDurationHistogramVec.WithLabelValues(c.Name(), strconv.FormatBool(hasError)).(prometheus.Histogram).Desc(),
			1,
			duration,
			nil,
			c.Name(),
			strconv.FormatBool(hasError),
		)
		return duration, hasError
	}

	duration = time.Since(start).Seconds() //TODO: is this duration correct?

	ch <- prometheus.MustNewConstHistogram(
		gathererDurationHistogramVec.WithLabelValues(c.Name(), strconv.FormatBool(hasError)).(prometheus.Histogram).Desc(),
		1,
		duration,
		nil,
		c.Name(),
		strconv.FormatBool(hasError),
	)

	return duration, hasError
}
