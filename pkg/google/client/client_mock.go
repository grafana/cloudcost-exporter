package client

import (
	"context"

	billingv1 "cloud.google.com/go/billing/apiv1"
	"cloud.google.com/go/billing/apiv1/billingpb"
	"cloud.google.com/go/storage"
	"github.com/grafana/cloudcost-exporter/pkg/google/client/cache"
	"github.com/grafana/cloudcost-exporter/pkg/google/metrics"
	"github.com/stretchr/testify/mock"
	"google.golang.org/api/compute/v1"
	sqladmin "google.golang.org/api/sqladmin/v1beta4"
)

type Mock struct {
	mock.Mock

	region   *Region
	billing  *Billing
	bucket   *Bucket
	compute  *Compute
	sqladmin *SQLAdmin
}

func NewMock(projectId string, discount int, regionsClient RegionsClient, bucketClient StorageClientInterface, billingClient *billingv1.CloudCatalogClient, computeService *compute.Service, sqladminService *sqladmin.Service) *Mock {
	return &Mock{
		region:   newRegion(projectId, discount, regionsClient),
		billing:  newBilling(billingClient),
		bucket:   newBucket(bucketClient, cache.NewNoopCache[[]*storage.BucketAttrs]()),
		compute:  newCompute(computeService),
		sqladmin: newSQLAdmin(sqladminService, projectId),
	}
}

func (c *Mock) GetServiceName(ctx context.Context, serviceName string) (string, error) {
	return c.billing.getServiceName(ctx, serviceName)
}

func (c *Mock) ExportRegionalDiscounts(ctx context.Context, m *metrics.Metrics) error {
	return c.region.exportRegionalDiscounts(ctx, m)
}

func (c *Mock) ExportGCPCostData(ctx context.Context, serviceName string, m *metrics.Metrics) float64 {
	return c.billing.exportBilling(ctx, serviceName, m)
}

func (c *Mock) ExportBucketInfo(ctx context.Context, projects []string, m *metrics.Metrics) error {
	return c.bucket.exportBucketInfo(ctx, projects, m)
}

func (c *Mock) GetPricing(ctx context.Context, serviceName string) []*billingpb.Sku {
	return c.billing.getPricing(ctx, serviceName)
}

func (c *Mock) GetZones(projectId string) ([]*compute.Zone, error) {
	return c.compute.getZones(projectId)
}

func (c *Mock) ListInstancesInZone(projectId, zone string) ([]*MachineSpec, error) {
	return c.compute.listInstancesInZone(projectId, zone)
}

func (c *Mock) ListDisks(ctx context.Context, projectId string, zone string) ([]*compute.Disk, error) {
	return c.compute.listDisks(ctx, projectId, zone)
}

func (c *Mock) GetRegions(project string) ([]*compute.Region, error) {
	return c.compute.getRegions(project)
}

func (c *Mock) ListForwardingRules(ctx context.Context, project string, region string) ([]*compute.ForwardingRule, error) {
	return c.compute.listForwardingRules(ctx, project, region)
}

func (c *Mock) ListSQLInstances(ctx context.Context, projectId string) ([]*sqladmin.DatabaseInstance, error) {
	return c.sqladmin.listInstances(ctx, projectId)
}
