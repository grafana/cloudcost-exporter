package utils

import (
	"github.com/prometheus/client_golang/prometheus"
	. "github.com/prometheus/client_model/go"
)

type LabelMap map[string]string

type MetricResult struct {
	Labels     LabelMap
	Value      float64
	MetricType prometheus.ValueType
}

func ReadMetrics(metric prometheus.Metric) *MetricResult {
	m := &Metric{}
	err := metric.Write(m)
	if err != nil {
		return nil
	}
	labels := make(LabelMap, len(m.Label))
	for _, l := range m.Label {
		labels[l.GetName()] = l.GetValue()
	}
	if m.Gauge != nil {
		return &MetricResult{
			Labels:     labels,
			Value:      m.GetGauge().GetValue(),
			MetricType: prometheus.GaugeValue,
		}
	}
	if m.Counter != nil {
		return &MetricResult{
			Labels:     labels,
			Value:      m.GetCounter().GetValue(),
			MetricType: prometheus.CounterValue,
		}
	}
	if m.Untyped != nil {
		return &MetricResult{
			Labels:     labels,
			Value:      m.GetUntyped().GetValue(),
			MetricType: prometheus.UntypedValue,
		}
	}
	return nil
}
