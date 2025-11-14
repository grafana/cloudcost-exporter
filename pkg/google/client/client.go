package client

import (
	"context"

	"cloud.google.com/go/billing/apiv1/billingpb"
	"github.com/grafana/cloudcost-exporter/pkg/google/metrics"
	"google.golang.org/api/compute/v1"
	sqladmin "google.golang.org/api/sqladmin/v1beta4"
)

type Client interface {
	GetServiceName(ctx context.Context, serviceName string) (string, error)
	ExportRegionalDiscounts(ctx context.Context, m *metrics.Metrics) error
	ExportGCPCostData(ctx context.Context, serviceName string, m *metrics.Metrics) float64
	ExportBucketInfo(ctx context.Context, projects []string, m *metrics.Metrics) error
	GetPricing(ctx context.Context, serviceName string) []*billingpb.Sku
	GetZones(project string) ([]*compute.Zone, error)
	GetRegions(project string) ([]*compute.Region, error)
	ListInstancesInZone(projectId, zone string) ([]*MachineSpec, error)
	ListDisks(ctx context.Context, project string, zone string) ([]*compute.Disk, error)
	ListForwardingRules(ctx context.Context, project string, region string) ([]*compute.ForwardingRule, error)
	ListSQLInstances(ctx context.Context, project string) ([]*sqladmin.DatabaseInstance, error)
}
