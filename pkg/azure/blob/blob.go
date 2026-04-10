package blob

import (
	"context"
	"log/slog"

	"github.com/grafana/cloudcost-exporter/pkg/provider"
	"github.com/prometheus/client_golang/prometheus"

	cloudcost_exporter "github.com/grafana/cloudcost-exporter"
)

const subsystem = "azure_blob"

// metrics holds Prometheus collectors for blob cost rates. Vectors stay empty until Cost Management data exists (no Set calls yet).
type metrics struct {
	StorageGauge *prometheus.GaugeVec
	// Planned future work: operation request rate (parity with S3/GCS cloudcost_*_operation_by_location_usd_per_krequest).
	// OperationsGauge *prometheus.GaugeVec
}

func newMetrics() metrics {
	m := metrics{
		StorageGauge: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: prometheus.BuildFQName(cloudcost_exporter.MetricPrefix, subsystem, "storage_by_location_usd_per_gibyte_hour"),
			Help: "Storage cost of Blob objects by region and class. Cost represented in USD/(GiB*h). No samples until Cost Management is integrated.",
		},
			[]string{"region", "class"},
		),
	}

	// Planned future work: register operation cost per 1k requests (labels region, class, tier) when Cost Management dimensions support it.
	// m.OperationsGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
	// 	Name: prometheus.BuildFQName(cloudcost_exporter.MetricPrefix, subsystem, "operation_by_location_usd_per_krequest"),
	// 	Help: "Operation cost of Blob objects by region, class, and tier. Cost represented in USD/(1k req). No samples until Cost Management is integrated.",
	// },
	// 	[]string{"region", "class", "tier"},
	// )

	return m
}

// Collector implements provider.Collector for Azure Blob Storage cost metrics.
// Cost Management integration is not implemented yet; registered metrics emit no series until Collect sets label values.
type Collector struct {
	logger  *slog.Logger
	metrics metrics
}

// Config holds settings for the blob collector.
type Config struct {
	Logger *slog.Logger
}

// New builds a blob collector. It does not call Azure APIs.
func New(cfg *Config) (*Collector, error) {
	return &Collector{
		logger:  cfg.Logger.With("collector", "blob"),
		metrics: newMetrics(),
	}, nil
}

// Collect satisfies provider.Collector. Does not call Set on cost vectors yet (no labeled series).
func (c *Collector) Collect(ctx context.Context, _ chan<- prometheus.Metric) error {
	c.logger.LogAttrs(ctx, slog.LevelInfo, "collecting metrics")
	return nil
}

// Describe satisfies provider.Collector.
func (c *Collector) Describe(ch chan<- *prometheus.Desc) error {
	c.metrics.StorageGauge.Describe(ch)
	// Planned future work: c.metrics.OperationsGauge.Describe(ch)
	return nil
}

// Name returns the collector subsystem name for operational metrics.
func (c *Collector) Name() string {
	return subsystem
}

// Register satisfies provider.Collector.
func (c *Collector) Register(registry provider.Registry) error {
	registry.MustRegister(c.metrics.StorageGauge)
	// Planned future work: registry.MustRegister(c.metrics.OperationsGauge)
	c.logger.LogAttrs(context.Background(), slog.LevelInfo, "registering collector")
	return nil
}
