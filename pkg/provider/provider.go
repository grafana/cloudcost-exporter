package provider

import (
	"context"
	"strings"

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

// CanonicalServiceName resolves a -{provider}.services flag value to the
// canonical Name of the matching ServiceInfo, matching Name and Aliases
// case-insensitively and ignoring surrounding whitespace. A value that matches
// no service is returned trimmed and upper-cased.
func CanonicalServiceName(services []ServiceInfo, value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	for _, s := range services {
		if strings.EqualFold(s.Name, value) {
			return s.Name
		}
		for _, alias := range s.Aliases {
			if strings.EqualFold(alias, value) {
				return s.Name
			}
		}
	}
	return value
}
