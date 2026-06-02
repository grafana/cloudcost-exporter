package gcs

import (
	"testing"

	"github.com/grafana/cloudcost-exporter/pkg/google/metrics"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
)

// TestExporterOperationsDiscounts_FansOutPerProject verifies that
// operations-discount series are emitted once per configured project so the
// project label is populated for each project, not just one. Regression
// test for the multi-project rollout of the project label.
func TestExporterOperationsDiscounts_FansOutPerProject(t *testing.T) {
	m := metrics.NewMetrics()
	projects := []string{"project-a", "project-b"}

	exporterOperationsDiscounts(projects, m)

	// Each unique (location_type, storage_class, opclass) tuple in
	// operationsDiscountMap should produce one series per project.
	var tuples int
	for _, byClass := range operationsDiscountMap {
		for _, byOps := range byClass {
			tuples += len(byOps)
		}
	}

	got := testutil.CollectAndCount(m.OperationsDiscountGauge)
	assert.Equal(t, tuples*len(projects), got,
		"expected one series per (locationType, storageClass, opClass, project) tuple")

	// Spot-check: the same (locationType, storage_class, opclass) tuple should
	// have a value for each project.
	for _, project := range projects {
		v := testutil.ToFloat64(m.OperationsDiscountGauge.WithLabelValues(project, "region", "STANDARD", "class-a"))
		assert.InDelta(t, 0.190, v, 1e-9, "missing series for project %q", project)
	}
}
