package provider

import (
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
	CheckReadiness() bool
	CollectMetrics(chan<- prometheus.Metric) float64
	Collect(chan<- prometheus.Metric) error
	Describe(chan<- *prometheus.Desc) error
	Name() string
}

type Provider interface {
	prometheus.Collector
	CheckReadiness() bool
	RegisterCollectors(r Registry) error
}
