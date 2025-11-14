package client

import (
	"context"
	"fmt"

	billingv1 "cloud.google.com/go/billing/apiv1"
	"cloud.google.com/go/billing/apiv1/billingpb"
	computeapiv1 "cloud.google.com/go/compute/apiv1"
	"cloud.google.com/go/storage"
	"github.com/grafana/cloudcost-exporter/pkg/google/client/cache"
	"github.com/grafana/cloudcost-exporter/pkg/google/metrics"
	computev1 "google.golang.org/api/compute/v1"
	sqladmin "google.golang.org/api/sqladmin/v1beta4"
)

type GCPClient struct {
	compute  *Compute
	billing  *Billing
	regions  *Region
	bucket   *Bucket
	sqlAdmin *SQLAdmin
}

type Config struct {
	ProjectId string
	Discount  int
}

func NewGCPClient(ctx context.Context, cfg Config) (*GCPClient, error) {
	computeService, err := computev1.NewService(ctx)
	if err != nil {
		return nil, fmt.Errorf("error creating compute computeService: %w", err)
	}

	cloudCatalogClient, err := billingv1.NewCloudCatalogClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("error creating cloudCatalogClient: %w", err)
	}

	regionsClient, err := computeapiv1.NewRegionsRESTClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not create regions client: %w", err)
	}

	storageClient, err := storage.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not create bucket client: %w", err)
	}

	sqlAdminClient, err := sqladmin.NewService(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not create sql admin client: %w", err)
	}

	return &GCPClient{
		compute:  newCompute(computeService),
		billing:  newBilling(cloudCatalogClient),
		regions:  newRegion(cfg.ProjectId, cfg.Discount, regionsClient),
		bucket:   newBucket(storageClient, cache.NewBucketCache()),
		sqlAdmin: newSQLAdmin(sqlAdminClient, cfg.ProjectId),
	}, nil
}

func (c *GCPClient) GetServiceName(ctx context.Context, serviceName string) (string, error) {
	return c.billing.getServiceName(ctx, serviceName)
}

func (c *GCPClient) ExportRegionalDiscounts(ctx context.Context, m *metrics.Metrics) error {
	return c.regions.exportRegionalDiscounts(ctx, m)
}

func (c *GCPClient) ExportGCPCostData(ctx context.Context, serviceName string, m *metrics.Metrics) float64 {
	return c.billing.exportBilling(ctx, serviceName, m)
}

func (c *GCPClient) GetPricing(ctx context.Context, serviceName string) []*billingpb.Sku {
	return c.billing.getPricing(ctx, serviceName)
}

func (c *GCPClient) ExportBucketInfo(ctx context.Context, projects []string, m *metrics.Metrics) error {
	return c.bucket.exportBucketInfo(ctx, projects, m)
}

func (c *GCPClient) GetZones(projectId string) ([]*computev1.Zone, error) {
	return c.compute.getZones(projectId)
}

func (c *GCPClient) GetRegions(projectId string) ([]*computev1.Region, error) {
	return c.compute.getRegions(projectId)
}

func (c *GCPClient) ListInstancesInZone(projectId, zone string) ([]*MachineSpec, error) {
	return c.compute.listInstancesInZone(projectId, zone)
}

func (c *GCPClient) ListDisks(ctx context.Context, projectId string, zone string) ([]*computev1.Disk, error) {
	return c.compute.listDisks(ctx, projectId, zone)
}

func (c *GCPClient) ListForwardingRules(ctx context.Context, projectId string, region string) ([]*computev1.ForwardingRule, error) {
	return c.compute.listForwardingRules(ctx, projectId, region)
}

func (c *GCPClient) ListSQLInstances(ctx context.Context, projectId string) ([]*sqladmin.DatabaseInstance, error) {
	return c.sqlAdmin.listInstances(ctx, projectId)
}
