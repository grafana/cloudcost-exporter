package utils

import (
	"regexp"

	"github.com/prometheus/client_golang/prometheus"
	io_prometheus_client "github.com/prometheus/client_model/go"
)

type LabelMap map[string]string

type MetricResult struct {
	FqName     string
	Labels     LabelMap
	Value      float64
	MetricType prometheus.ValueType
}

var (
	re = regexp.MustCompile(`fqName:\s*"([^"]+)"`)
)

func ReadMetrics(metric prometheus.Metric) *MetricResult {
	if metric == nil {
		return nil
	}
	m := &io_prometheus_client.Metric{}
	err := metric.Write(m)
	if err != nil {
		return nil
	}
	labels := make(LabelMap, len(m.Label))
	for _, l := range m.Label {
		labels[l.GetName()] = l.GetValue()
	}
	fqName := parseFqNameFromMetric(metric.Desc().String())
	if m.Gauge != nil {
		return &MetricResult{
			FqName:     fqName,
			Labels:     labels,
			Value:      m.GetGauge().GetValue(),
			MetricType: prometheus.GaugeValue,
		}
	}
	if m.Counter != nil {
		return &MetricResult{
			FqName:     fqName,
			Labels:     labels,
			Value:      m.GetCounter().GetValue(),
			MetricType: prometheus.CounterValue,
		}
	}
	if m.Untyped != nil {
		return &MetricResult{
			FqName:     fqName,
			Labels:     labels,
			Value:      m.GetUntyped().GetValue(),
			MetricType: prometheus.UntypedValue,
		}
	}
	return nil
}

func parseFqNameFromMetric(desc string) string {
	if desc == "" {
		return ""
	}
	return re.FindStringSubmatch(desc)[1]
}
