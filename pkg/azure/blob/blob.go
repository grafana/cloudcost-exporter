package blob

import (
	"context"
	"log/slog"

	"github.com/grafana/cloudcost-exporter/pkg/provider"
	"github.com/prometheus/client_golang/prometheus"
)

const subsystem = "azure_blob"

// Collector implements provider.Collector for Azure Blob Storage cost metrics.
// Cost Management integration is not implemented yet; Collect emits no cost series.
type Collector struct {
	logger *slog.Logger
}

// Config holds settings for the blob collector.
type Config struct {
	Logger *slog.Logger
}

// New builds a blob collector. It does not call Azure APIs.
func New(cfg *Config) (*Collector, error) {
	return &Collector{
		logger: cfg.Logger.With("collector", "blob"),
	}, nil
}

// Collect satisfies provider.Collector. No Prometheus samples are sent until Cost Management is wired in.
func (c *Collector) Collect(ctx context.Context, _ chan<- prometheus.Metric) error {
	c.logger.LogAttrs(ctx, slog.LevelInfo, "collecting metrics")
	return nil
}

// Describe satisfies provider.Collector. No metric descriptors until cost gauges are implemented.
func (c *Collector) Describe(_ chan<- *prometheus.Desc) error {
	return nil
}

// Name returns the collector subsystem name for operational metrics.
func (c *Collector) Name() string {
	return subsystem
}

// Register satisfies provider.Collector.
func (c *Collector) Register(_ provider.Registry) error {
	c.logger.LogAttrs(context.Background(), slog.LevelInfo, "registering collector")
	return nil
}
