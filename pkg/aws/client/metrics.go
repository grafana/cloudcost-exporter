package client

import (
	cloudcost_exporter "github.com/grafana/cloudcost-exporter"
	"github.com/prometheus/client_golang/prometheus"
)

const subsystem = "aws_s3"

type Metrics struct {
	// RequestCount is a counter that tracks the number of requests made to the AWS Cost Explorer API
	RequestCount prometheus.Counter

	// RequestErrorsCount is a counter that tracks the number of errors when making requests to the AWS Cost Explorer API
	RequestErrorsCount prometheus.Counter
}

func NewMetrics() *Metrics {
	return &Metrics{
		RequestCount: prometheus.NewCounter(prometheus.CounterOpts{
			Name: prometheus.BuildFQName(cloudcost_exporter.ExporterName, subsystem, "cost_api_requests_total"),
			Help: "Total number of requests made to the AWS Cost Explorer API",
		}),

		RequestErrorsCount: prometheus.NewCounter(prometheus.CounterOpts{
			Name: prometheus.BuildFQName(cloudcost_exporter.ExporterName, subsystem, "cost_api_requests_errors_total"),
			Help: "Total number of errors when making requests to the AWS Cost Explorer API",
		}),
	}
}
