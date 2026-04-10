package blob

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cloudcost_exporter "github.com/grafana/cloudcost-exporter"
)

var testLogger = slog.New(slog.NewTextHandler(os.Stdout, nil))

func testCollectSink() chan prometheus.Metric {
	return make(chan prometheus.Metric, 8)
}

type stubCostQuerier struct {
	rows []StorageCostRow
	err  error
}

func (s stubCostQuerier) QueryBlobStorage(context.Context, string, time.Duration) ([]StorageCostRow, error) {
	return s.rows, s.err
}

func TestCollector_Collect_queryError(t *testing.T) {
	c, err := New(&Config{
		Logger:         testLogger,
		SubscriptionId: "sub",
		CostQuerier:    stubCostQuerier{err: errors.New("query failed")},
	})
	require.NoError(t, err)
	assert.Error(t, c.Collect(t.Context(), testCollectSink()))
}

func TestCollector_Collect_setsGaugeFromQuerier(t *testing.T) {
	c, err := New(&Config{
		Logger:         testLogger,
		SubscriptionId: "sub",
		CostQuerier: stubCostQuerier{rows: []StorageCostRow{
			{Region: "eastus", Class: "Hot", Rate: 0.002},
		}},
	})
	require.NoError(t, err)
	require.NoError(t, c.Collect(t.Context(), testCollectSink()))
	err = testutil.CollectAndCompare(c.metrics.StorageGauge, strings.NewReader(`
# HELP cloudcost_azure_blob_storage_by_location_usd_per_gibyte_hour Storage cost of blob objects by region and class. Cost represented in USD/(GiB*h). Populated when CostQuerier returns data.
# TYPE cloudcost_azure_blob_storage_by_location_usd_per_gibyte_hour gauge
cloudcost_azure_blob_storage_by_location_usd_per_gibyte_hour{class="Hot",region="eastus"} 0.002
`), "cloudcost_azure_blob_storage_by_location_usd_per_gibyte_hour")
	require.NoError(t, err)
}

func TestCollector_Describe(t *testing.T) {
	c, err := New(&Config{Logger: testLogger})
	require.NoError(t, err)
	ch := make(chan *prometheus.Desc, 4)
	require.NoError(t, c.Describe(ch))
	close(ch)
	var got []string
	for d := range ch {
		got = append(got, d.String())
	}
	require.Len(t, got, 1)
	joined := strings.Join(got, " ")
	prefix := cloudcost_exporter.MetricPrefix + "_azure_blob_"
	assert.Contains(t, joined, prefix+"storage_by_location_usd_per_gibyte_hour")
}

// #TODO: remove when we have more functionality in place
func TestCollector_Register(t *testing.T) {
	c, err := New(&Config{Logger: testLogger})
	require.NoError(t, err)
	// Register does not call registry.MustRegister on cost metrics (AKS pattern).
	require.NoError(t, c.Register(nil))
}

// #TODO: remove when we have more functionality in place
func TestCollector_Collect_forwardsStorageGauge(t *testing.T) {
	c, err := New(&Config{Logger: testLogger})
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, c.Collect(ctx, testCollectSink()))
}

func TestNew_configPlumbing(t *testing.T) {
	const subUUID = "11111111-1111-1111-1111-111111111111"
	tests := map[string]struct {
		subscriptionID string
		scrapeInterval time.Duration
		wantInterval   time.Duration
	}{
		"zero scrape interval defaults to one hour": {
			subscriptionID: "sub-1",
			scrapeInterval: 0,
			wantInterval:   time.Hour,
		},
		"explicit subscription and interval": {
			subscriptionID: subUUID,
			scrapeInterval: 30 * time.Minute,
			wantInterval:   30 * time.Minute,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			c, err := New(&Config{
				Logger:         testLogger,
				SubscriptionId: tt.subscriptionID,
				ScrapeInterval: tt.scrapeInterval,
			})
			require.NoError(t, err)
			assert.Equal(t, tt.subscriptionID, c.subscriptionID)
			assert.Equal(t, tt.wantInterval, c.scrapeInterval)
		})
	}
}
