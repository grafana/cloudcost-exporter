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

// ServiceEntry is a service to register, tagged with whether it came from a provider's
// experimental flag (-{provider}.experimental.services) and so is exempt from the
// backward-compatibility contract.
type ServiceEntry struct {
	Name         string
	Experimental bool
}

// MergeServiceEntries merges a provider's stable and experimental service lists into one ordered
// set. It trims names and drops empties (an unset flag round-trips as [""] from the flag's
// String/Split handling), and skips an experimental entry already enabled as stable
// (case-insensitive) so a collector graduating from experimental to stable never registers twice.
func MergeServiceEntries(stable, experimental []string) []ServiceEntry {
	entries := make([]ServiceEntry, 0, len(stable)+len(experimental))
	stableNames := make(map[string]bool, len(stable))
	for _, name := range stable {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		stableNames[strings.ToUpper(name)] = true
		entries = append(entries, ServiceEntry{Name: name})
	}
	for _, name := range experimental {
		name = strings.TrimSpace(name)
		if name == "" || stableNames[strings.ToUpper(name)] {
			continue
		}
		entries = append(entries, ServiceEntry{Name: name, Experimental: true})
	}
	return entries
}
