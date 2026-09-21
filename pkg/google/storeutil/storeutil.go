// Package storeutil holds background-refresh helpers shared by GCP collectors
// that maintain their own periodically-refreshed in-memory stores (gke, gce).
package storeutil

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	cloudcostexporter "github.com/grafana/cloudcost-exporter"
)

// NewPopulateErrorsCounter returns a counter vector for tracking background
// store population errors for the given subsystem, labeled by store,
// project, and operation.
func NewPopulateErrorsCounter(subsystem string) *prometheus.CounterVec {
	return prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: prometheus.BuildFQName(cloudcostexporter.ExporterName, subsystem, "populate_errors_total"),
			Help: "Total errors during background store population, by store, project, and operation.",
		},
		[]string{"store", "project", "operation"},
	)
}

// StartRefreshTicker runs run on every tick of interval until ctx is done.
func StartRefreshTicker(ctx context.Context, interval time.Duration, run func()) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				run()
			}
		}
	}()
}
