package collectormetrics

import (
	"context"
	"log/slog"
	"strings"
	"time"

	cloudcost_exporter "github.com/grafana/cloudcost-exporter"
	"github.com/grafana/cloudcost-exporter/pkg/provider"
	"github.com/grafana/cloudcost-exporter/pkg/utils"
	"github.com/prometheus/client_golang/prometheus"
)

var durationHistogramVec = prometheus.NewHistogramVec(
	prometheus.HistogramOpts{
		Name:                           prometheus.BuildFQName(cloudcost_exporter.ExporterName, "collector", "duration_seconds"),
		Help:                           "Duration of a collector scrape in seconds",
		NativeHistogramBucketFactor:    1.1,
		NativeHistogramMaxBucketNumber: 100,
	},
	[]string{"collector", "provider", "region"},
)

var errorCounterVec = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: prometheus.BuildFQName(cloudcost_exporter.ExporterName, "collector", "error"),
		Help: "Total number of errors that occurred during the last scrape.",
	},
	[]string{"collector", "provider", "region"},
)

var totalCounterVec = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: prometheus.BuildFQName(cloudcost_exporter.ExporterName, "collector", "total"),
		Help: "Total number of scrapes.",
	},
	[]string{"collector", "provider", "region"},
)

func emitOperationalMetrics(ch chan<- prometheus.Metric, collectorName string, providerName string, region string, duration float64) {
	h := durationHistogramVec.WithLabelValues(collectorName, providerName, region).(prometheus.Histogram)
	h.Observe(duration)
	ch <- h

	counter := totalCounterVec.WithLabelValues(collectorName, providerName, region)
	counter.Inc()
	ch <- counter
}

// Collect collects metrics from a collector and emits operational metrics to the channel.
func Collect(ctx context.Context, c provider.Collector, ch chan<- prometheus.Metric, logger *slog.Logger, providerName string) (float64, bool) {
	start := time.Now()
	var hasError bool
	var duration float64

	collectErr := c.Collect(ctx, ch)
	duration = time.Since(start).Seconds()

	regions := []string{utils.RegionUnknown}
	if rp, ok := c.(provider.RegionsProvider); ok {
		if r := rp.Regions(); len(r) > 0 {
			regions = r
		}
	}

	if collectErr != nil {
		hasError = true
		logger.LogAttrs(ctx, slog.LevelError, "could not collect metrics",
			slog.String("collector", c.Name()),
			slog.String("error", escapeLineBreaks(collectErr.Error())),
		)
		for _, region := range regions {
			errorCounter := errorCounterVec.WithLabelValues(c.Name(), providerName, region)
			errorCounter.Inc()
			ch <- errorCounter
		}
	}

	for _, region := range regions {
		emitOperationalMetrics(ch, c.Name(), providerName, region, duration)
	}

	return duration, hasError
}

func escapeLineBreaks(message string) string {
	replacer := strings.NewReplacer(
		"\n", "\\n",
		"\r", "\\r",
	)
	return replacer.Replace(message)
}
