// Package utils contains shared helpers for metric construction and constants.
package utils

import (
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	// DefaultScrapeInterval is the default scrape interval for the collectors.
	DefaultScrapeInterval = 1 * time.Hour
	// HoursInMonth is an approximate average number of hours in a month.
	// 24.35 is the average number of hours per day over a year.
	HoursInMonth = 24.35 * 30
	// InstanceCPUCostSuffix is the suffix for per-core-hour CPU cost metrics.
	InstanceCPUCostSuffix = "instance_cpu_usd_per_core_hour"
	// InstanceMemoryCostSuffix is the suffix for per-GiB-hour memory cost metrics.
	InstanceMemoryCostSuffix = "instance_memory_usd_per_gib_hour"
	// InstanceTotalCostSuffix is the suffix for total per-hour instance cost metrics.
	InstanceTotalCostSuffix = "instance_total_usd_per_hour"
	// PersistentVolumeCostSuffix is the suffix for per-hour EBS volume cost metrics.
	PersistentVolumeCostSuffix = "persistent_volume_usd_per_hour"
	// Used for Azure persistent volumes following AWS naming pattern.
	PersistentVolumeCostPerGiBSuffix = "persistent_volume_usd_per_gib_hour"
	// RegionUnknown is used as a label value when a region or other attribute cannot be determined.
	RegionUnknown = "unknown"
	// InputTokenCostSuffix is the suffix for per-1k-input-token cost metrics.
	InputTokenCostSuffix = "input_usd_per_1k_tokens"
	// OutputTokenCostSuffix is the suffix for per-1k-output-token cost metrics.
	OutputTokenCostSuffix = "output_usd_per_1k_tokens"
	// SearchUnitCostSuffix is the suffix for per-1k-search-unit cost metrics (e.g. Cohere Rerank).
	SearchUnitCostSuffix = "search_unit_usd_per_1k_search_units"
)

// RegionsFromMap returns the keys of a map as a slice of strings.
// Used by collectors that store per-region clients in a map[string]Client.
func RegionsFromMap[V any](m map[string]V) []string {
	return slices.Collect(maps.Keys(m))
}

// GenerateDesc creates a Prometheus metric descriptor with a standardized fqname.
func GenerateDesc(prefix, subsystem, suffix, description string, labels []string) *prometheus.Desc {
	return prometheus.NewDesc(
		prometheus.BuildFQName(prefix, subsystem, suffix),
		description,
		labels,
		nil,
	)
}

// VariableLabelsFromDesc extracts the variable label names from a Prometheus
// Desc by parsing its String() representation. Intended for tests that need to
// assert label-set invariants across many descs without exposing private vars.
func VariableLabelsFromDesc(d *prometheus.Desc) []string {
	s := d.String()
	const marker = "variableLabels: {"
	start := strings.Index(s, marker)
	if start < 0 {
		return nil
	}
	start += len(marker)
	end := strings.Index(s[start:], "}")
	if end < 0 {
		return nil
	}
	inner := s[start : start+end]
	if inner == "" {
		return nil
	}
	return strings.Split(inner, ",")
}
