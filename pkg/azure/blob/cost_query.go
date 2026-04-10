package blob

import (
	"context"
	"time"
)

// defaultQueryLookback is how far back to request cost usage (similar to the S3 billing window).
const defaultQueryLookback = 30 * 24 * time.Hour

// StorageCostRow is one region/class storage rate from a cost data source.
type StorageCostRow struct {
	Region string
	Class  string
	Rate   float64 // USD per GiB per hour
}

// StorageCostQuerier loads blob storage cost rates for a subscription (e.g. Azure Cost Management).
// A real implementation can be wired via Config.CostQuerier; default is a no-op.
type StorageCostQuerier interface {
	QueryBlobStorage(ctx context.Context, subscriptionID string, lookback time.Duration) ([]StorageCostRow, error)
}

type noopStorageCostQuerier struct{}

func (noopStorageCostQuerier) QueryBlobStorage(context.Context, string, time.Duration) ([]StorageCostRow, error) {
	return nil, nil
}
