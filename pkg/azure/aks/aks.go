package aks

import (
	"context"
	"log/slog"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/cloudcost-exporter/pkg/provider"
)

const (
	subsystem = "azure_aks"
)

// Errors
var (
// TODO - define Errors
)

// Prometheus Metrics
var (
// TODO - define Prometheus Metrics
)

// Collector is a prometheus collector that collects metrics from AKS clusters.
type Collector struct {
	Context context.Context
	Logger  *slog.Logger
}

type Config struct {
	Logger *slog.Logger
}

func New(ctx context.Context, cfg *Config) *Collector {
	logger := cfg.Logger.With("collector", subsystem)

	return &Collector{
		Logger: logger,
	}
}

// CollectMetrics is a no-op function that satisfies the provider.Collector interface.
// Deprecated: CollectMetrics is deprecated and will be removed in a future release.
func (c *Collector) CollectMetrics(_ chan<- prometheus.Metric) float64 {
	return 0
}

// Collect satisfies the provider.Collector interface.
func (c *Collector) Collect(ch chan<- prometheus.Metric) error {
	// TODO - implement
	c.Logger.LogAttrs(c.Context, slog.LevelInfo, "TODO - implement AKS collector Collect method")
	return nil
}

func (c *Collector) Describe(ch chan<- *prometheus.Desc) error {
	// TODO - implement
	c.Logger.LogAttrs(c.Context, slog.LevelInfo, "TODO - implement AKS collector Describe method")
	return nil
}

func (c *Collector) Name() string {
	return subsystem
}

func (c *Collector) Register(_ provider.Registry) error {
	c.Logger.LogAttrs(c.Context, slog.LevelInfo, "registering collector")
	return nil
}
