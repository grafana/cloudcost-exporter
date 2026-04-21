package blob

import (
	"context"
	"log/slog"
	"time"

	"github.com/grafana/cloudcost-exporter/pkg/provider"
	"github.com/prometheus/client_golang/prometheus"

	cloudcost_exporter "github.com/grafana/cloudcost-exporter"
)

const subsystem = "azure_blob"

// metrics holds Prometheus collectors for blob cost rates. Vectors are not registered on the root registry;
// Azure's top-level Collector gathers them via Collect → GaugeVec.Collect (same pattern as pkg/azure/aks).
type metrics struct {
	StorageGauge *prometheus.GaugeVec
}

func newMetrics() metrics {
	m := metrics{
		StorageGauge: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: prometheus.BuildFQName(cloudcost_exporter.MetricPrefix, subsystem, "storage_by_location_usd_per_gibyte_hour"),
			Help: "Storage cost of blob objects by region and class. Cost represented in USD/(GiB*h). No samples until Cost Management is integrated.",
		},
			[]string{"region", "class"},
		),
	}

	return m
}

// Collector implements provider.Collector for Azure Blob Storage cost metrics.
// Cost Management integration is not implemented yet; there are no labeled series until Collect calls Set on the vec.
type Collector struct {
	logger         *slog.Logger
	metrics        metrics
	subscriptionID string
	scrapeInterval time.Duration
}

// Config holds settings for the blob collector.
type Config struct {
	SubscriptionID string
	ScrapeInterval time.Duration
}

// New builds a blob collector. It does not call Azure APIs yet; subscription and interval are stored for Cost Management integration.
// TODO: Add a provider client parameter (e.g. azClientWrapper) once Cost Management integration is implemented,
// to match the standard Azure constructor signature: New(ctx, cfg, client).
func New(ctx context.Context, cfg *Config, logger *slog.Logger) (*Collector, error) {
	interval := cfg.ScrapeInterval
	if interval <= 0 {
		interval = time.Hour
	}
	return &Collector{
		logger:         logger.With("collector", "blob"),
		metrics:        newMetrics(),
		subscriptionID: cfg.SubscriptionID,
		scrapeInterval: interval,
	}, nil
}

// Collect satisfies provider.Collector. Does not call Set on cost vectors yet; still forwards the vec on ch for the parent gatherer.
func (c *Collector) Collect(ctx context.Context, ch chan<- prometheus.Metric) error {
	c.logger.LogAttrs(ctx, slog.LevelInfo, "collecting metrics")
	c.metrics.StorageGauge.Collect(ch)
	return nil
}

// Describe satisfies provider.Collector.
func (c *Collector) Describe(ch chan<- *prometheus.Desc) error {
	c.metrics.StorageGauge.Describe(ch)
	return nil
}

// Name returns the collector subsystem name for operational metrics.
func (c *Collector) Name() string {
	return subsystem
}

// Register satisfies provider.Collector. Does not register cost metrics on the registry (avoids duplicate Desc
// with Azure's Describe fan-out; metrics are collected via Collect → StorageGauge.Collect).
func (c *Collector) Register(_ provider.Registry) error {
	c.logger.LogAttrs(context.Background(), slog.LevelInfo, "registering collector")
	return nil
}
