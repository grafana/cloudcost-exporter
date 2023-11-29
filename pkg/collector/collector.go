package collector

import (
	"github.com/prometheus/client_golang/prometheus"
)

type Collector interface {
	Register(*prometheus.Registry) error
	Collect() error
	Name() string
}
type CSP interface {
	RegisterCollectors(registry *prometheus.Registry) error
	CollectMetrics() error
}
