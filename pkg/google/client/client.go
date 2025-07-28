package client

import (
	"context"

	"github.com/grafana/cloudcost-exporter/pkg/google/metrics"
)

type Client interface {
	GetServiceName(ctx context.Context, serviceName string) (string, error)
	ExportRegionalDiscounts(ctx context.Context, m *metrics.Metrics) error
	ExportGCPCostData(ctx context.Context, serviceName string, m *metrics.Metrics) float64
	ExportBucketInfo(ctx context.Context, projects []string, m *metrics.Metrics) error
}
