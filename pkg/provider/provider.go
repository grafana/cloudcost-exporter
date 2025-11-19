package provider

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"
)

//go:generate mockgen -source=provider.go -destination mocks/provider.go

type Registry interface {
	prometheus.Registerer
	prometheus.Gatherer
	prometheus.Collector
}

type Collector interface {
	Register(r Registry) error
	CollectMetrics(chan<- prometheus.Metric) float64
	Collect(ctx context.Context, ch chan<- prometheus.Metric) error
	Describe(chan<- *prometheus.Desc) error
	Name() string
}

type Provider interface {
	prometheus.Collector
	RegisterCollectors(r Registry) error
}
