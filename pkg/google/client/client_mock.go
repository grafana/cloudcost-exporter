package client

import (
	"context"

	billingv1 "cloud.google.com/go/billing/apiv1"
	"cloud.google.com/go/storage"
	"github.com/grafana/cloudcost-exporter/pkg/google/client/cache"
	"github.com/grafana/cloudcost-exporter/pkg/google/metrics"
	"github.com/stretchr/testify/mock"
)

type Mock struct {
	mock.Mock

	region  *Region
	billing *Billing
	bucket  *Bucket
}

func NewMock(projectId string, discount int, regionsClient RegionsClient, bucketClient StorageClientInterface, billingClient *billingv1.CloudCatalogClient) *Mock {
	return &Mock{
		region:  newRegion(projectId, discount, regionsClient),
		billing: newBilling(billingClient),
		bucket:  newBucket(bucketClient, cache.NewNoopCache[[]*storage.BucketAttrs]()),
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
