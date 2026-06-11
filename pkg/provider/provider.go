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
	Collect(ctx context.Context, ch chan<- prometheus.Metric) error
	Describe(chan<- *prometheus.Desc) error
	Name() string
}

type RegionsProvider interface {
	Regions() []string
}

type Provider interface {
	prometheus.Collector
	RegisterCollectors(r Registry) error
}

// ServiceInfo describes a collector that a provider can register via its
// -{provider}.services flag. Each provider exposes a Services() function
// returning these entries; the same Name is used by the dispatch switch and
// the -list-services flag, and verified against docs/metrics/README.md by a
// drift test.
type ServiceInfo struct {
	Name        string
	DisplayName string
	Description string
	Aliases     []string
}
