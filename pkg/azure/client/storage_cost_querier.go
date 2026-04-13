package client

import (
	"context"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/costmanagement/armcostmanagement"

	"github.com/grafana/cloudcost-exporter/pkg/azure/blob"
)

var _ blob.StorageCostQuerier = (*BlobStorageCostQuerier)(nil)

// BlobStorageCostQuerier implements blob.StorageCostQuerier using Azure Cost Management QueryClient.
// QueryBlobStorage returns no rows until a subscription-scoped Usage query is implemented.
type BlobStorageCostQuerier struct {
	query *armcostmanagement.QueryClient
}

// NewBlobStorageCostQuerier builds a querier backed by armcostmanagement.
func NewBlobStorageCostQuerier(credential azcore.TokenCredential, options *arm.ClientOptions) (*BlobStorageCostQuerier, error) {
	factory, err := armcostmanagement.NewClientFactory(credential, options)
	if err != nil {
		return nil, err
	}
	return &BlobStorageCostQuerier{query: factory.NewQueryClient()}, nil
}

// CostQueryClient returns the underlying Cost Management client for subscription-scoped Usage calls.
func (q *BlobStorageCostQuerier) CostQueryClient() *armcostmanagement.QueryClient {
	return q.query
}

// QueryBlobStorage implements blob.StorageCostQuerier.
func (*BlobStorageCostQuerier) QueryBlobStorage(context.Context, string, time.Duration) ([]blob.StorageCostRow, error) {
	return nil, nil
}
