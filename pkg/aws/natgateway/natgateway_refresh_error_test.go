package natgateway

// White-box tests that need direct access to unexported fields (lastRefreshErr).
// External tests live in natgateway_test.go (package natgateway_test).

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/cloudcost-exporter/pkg/aws/pricingstore"
)

// stubPricingStore is a minimal PricingStoreRefresher for internal tests.
// It returns an empty snapshot and never fails on refresh.
type stubPricingStore struct{}

func (s *stubPricingStore) PopulatePricingMap(_ context.Context) error { return nil }
func (s *stubPricingStore) Snapshot() pricingstore.Snapshot            { return pricingstore.Snapshot{} }

func TestCollect_EmitsRefreshErrorGaugeAsOneAfterBackgroundFailure(t *testing.T) {
	c := &Collector{
		logger:       slog.New(slog.NewTextHandler(os.Stdout, nil)),
		PricingStore: &stubPricingStore{},
	}
	c.lastRefreshErr.Store(true)

	// Empty snapshot means only the PricingRefreshErrorDesc gauge is emitted before the error.
	ch := make(chan prometheus.Metric, 1)
	err := c.Collect(t.Context(), ch)
	close(ch)

	require.Error(t, err)

	var metrics []prometheus.Metric
	for m := range ch {
		metrics = append(metrics, m)
	}

	expected := prometheus.MustNewConstMetric(PricingRefreshErrorDesc, prometheus.GaugeValue, 1.0)
	require.Len(t, metrics, 1)
	assert.Equal(t, expected, metrics[0])
}
