package utils

import "github.com/prometheus/client_golang/prometheus"

const (
	HoursInMonth               = 24.35 * 30 // 24.35 is the average amount of hours in a day over a year
	InstanceCPUCostSuffix      = "instance_cpu_usd_per_core_hour"
	InstanceMemoryCostSuffix   = "instance_memory_usd_per_gib_hour"
	InstanceTotalCostSuffix    = "instance_total_usd_per_hour"
	PersistentVolumeCostSuffix = "persistent_volume_usd_per_hour"
)

func GenerateDesc(prefix, subsystem, suffix, description string, labels []string) *prometheus.Desc {
	return prometheus.NewDesc(
		prometheus.BuildFQName(prefix, subsystem, suffix),
		description,
		labels,
		nil,
	)
}