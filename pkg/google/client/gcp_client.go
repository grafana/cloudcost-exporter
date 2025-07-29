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
)

type GPCClient struct {
	compute *Compute
	billing *Billing
	regions *Region
	bucket  *Bucket
}

type Config struct {
	ProjectId string
	Discount  int
}

func NewGPCClient(ctx context.Context, cfg Config) (*GPCClient, error) {
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

	return &GPCClient{
		compute: newCompute(computeService),
		billing: newBilling(cloudCatalogClient),
		regions: newRegion(cfg.ProjectId, cfg.Discount, regionsClient),
		bucket:  newBucket(storageClient, cache.NewBucketCache()),
	}, nil
}

func (c *GPCClient) GetServiceName(ctx context.Context, serviceName string) (string, error) {
	return c.billing.getServiceName(ctx, serviceName)
}

func (c *GPCClient) ExportRegionalDiscounts(ctx context.Context, m *metrics.Metrics) error {
	return c.regions.exportRegionalDiscounts(ctx, m)
}

func (c *GPCClient) ExportGCPCostData(ctx context.Context, serviceName string, m *metrics.Metrics) float64 {
	return c.billing.exportBilling(ctx, serviceName, m)
}

func (c *GPCClient) GetPricing(ctx context.Context, serviceName string) []*billingpb.Sku {
	return c.billing.getPricing(ctx, serviceName)
}

func (c *GPCClient) ExportBucketInfo(ctx context.Context, projects []string, m *metrics.Metrics) error {
	return c.bucket.exportBucketInfo(ctx, projects, m)
}

func (c *GPCClient) GetZones(projectId string) ([]*computev1.Zone, error) {
	return c.compute.getZones(projectId)
}

func (c *GPCClient) ListInstancesInZone(projectId, zone string) ([]*MachineSpec, error) {
	return c.compute.listInstancesInZone(projectId, zone)
}

func (c *GPCClient) ListDisks(ctx context.Context, projectId string, zone string) ([]*computev1.Disk, error) {
	return c.compute.listDisks(ctx, projectId, zone)
}
