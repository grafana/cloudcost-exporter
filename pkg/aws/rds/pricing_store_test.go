package rds

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/grafana/cloudcost-exporter/pkg/aws/client"
	mock "github.com/grafana/cloudcost-exporter/pkg/aws/client/mocks"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// newTestPricingStore builds a store without the background goroutine the
// production constructor starts, so tests can drive Populate synchronously.
func newTestPricingStore(regions []types.Region, pricingClient client.Client) *pricingStore {
	return &pricingStore{
		logger:            slog.Default(),
		pricingClient:     pricingClient,
		regions:           regions,
		concurrency:       populateConcurrency,
		populateErrors:    newPopulateErrorsCounter(),
		prices:            newPricingMap(),
		initialPopulation: make(chan struct{}),
	}
}

// TestPricingStore_Populate_WarmsPricing verifies a populate fills the
// pricing map off the scrape path.
func TestPricingStore_Populate_WarmsPricing(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	pricingClient := mock.NewMockClient(mockCtrl)
	pricingClient.EXPECT().ListRDSPrices(gomock.Any(), gomock.Any()).
		Return([]string{postgresPrice("us-east-1", "0.456")}, nil).
		Times(1)

	regions := []types.Region{{RegionName: aws.String("us-east-1")}}
	store := newTestPricingStore(regions, pricingClient)

	store.Populate(t.Context())

	price, ok := store.Get(warmKey)
	assert.True(t, ok, "price should be warmed during populate")
	assert.Equal(t, 0.456, price)
}

// TestPricingStore_Populate_OneCallPerRegion verifies pricing is listed in
// bulk exactly once per configured region per populate.
func TestPricingStore_Populate_OneCallPerRegion(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	pricingClient := mock.NewMockClient(mockCtrl)
	expectPricing(pricingClient, "0.456")

	regions := []types.Region{
		{RegionName: aws.String("us-east-1")},
		{RegionName: aws.String("eu-west-1")},
	}
	store := newTestPricingStore(regions, pricingClient)

	store.Populate(t.Context())

	_, ok := store.Get(warmKey)
	assert.True(t, ok)
}

// TestPricingStore_Populate_Refresh verifies a second populate re-lists
// prices so the map tracks the latest rates.
func TestPricingStore_Populate_Refresh(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	pricingClient := mock.NewMockClient(mockCtrl)
	gomock.InOrder(
		pricingClient.EXPECT().ListRDSPrices(gomock.Any(), gomock.Any()).
			Return([]string{postgresPrice("us-east-1", "0.456")}, nil).Times(1),
		pricingClient.EXPECT().ListRDSPrices(gomock.Any(), gomock.Any()).
			Return([]string{postgresPrice("us-east-1", "0.789")}, nil).Times(1),
	)

	regions := []types.Region{{RegionName: aws.String("us-east-1")}}
	store := newTestPricingStore(regions, pricingClient)

	store.Populate(t.Context())
	price, _ := store.Get(warmKey)
	assert.Equal(t, 0.456, price)

	store.Populate(t.Context())
	price, _ = store.Get(warmKey)
	assert.Equal(t, 0.789, price, "refresh should overwrite the cached price")
}

// TestPricingStore_Populate_FailedRefreshKeepsStalePrice verifies a region
// that previously warmed successfully keeps serving its last-known-good price
// when a later refresh for that region fails, rather than losing the price.
func TestPricingStore_Populate_FailedRefreshKeepsStalePrice(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	pricingClient := mock.NewMockClient(mockCtrl)
	gomock.InOrder(
		pricingClient.EXPECT().ListRDSPrices(gomock.Any(), gomock.Any()).
			Return([]string{postgresPrice("us-east-1", "0.456")}, nil).Times(1),
		pricingClient.EXPECT().ListRDSPrices(gomock.Any(), gomock.Any()).
			Return(nil, errors.New("boom")).Times(1),
	)

	regions := []types.Region{{RegionName: aws.String("us-east-1")}}
	store := newTestPricingStore(regions, pricingClient)

	store.Populate(t.Context())
	price, ok := store.Get(warmKey)
	require.True(t, ok, "price should be warmed during the first populate")
	assert.Equal(t, 0.456, price)

	store.Populate(t.Context())
	price, ok = store.Get(warmKey)
	assert.True(t, ok, "a failed refresh should keep serving the last-known-good price")
	assert.Equal(t, 0.456, price)
	assert.Equal(t, 1.0, testutil.ToFloat64(store.populateErrors.WithLabelValues("pricing", "us-east-1", "list_prices")))
}

// TestPricingStore_Done_ClosesAfterPopulate verifies readiness is signalled
// once the first populate attempt finishes.
func TestPricingStore_Done_ClosesAfterPopulate(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	pricingClient := mock.NewMockClient(mockCtrl)
	expectPricing(pricingClient, "0.456")

	regions := []types.Region{{RegionName: aws.String("us-east-1")}}
	store := newTestPricingStore(regions, pricingClient)

	select {
	case <-store.Done():
		t.Fatal("Done should not be closed before the first populate")
	default:
	}

	store.Populate(t.Context())

	select {
	case <-store.Done():
	default:
		t.Fatal("Done should be closed after the first populate")
	}
}

// TestPricingStore_Populate_OverlapGuard verifies a populate is skipped while
// another is still running, so a slow AWS API cannot double the load.
func TestPricingStore_Populate_OverlapGuard(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	pricingClient := mock.NewMockClient(mockCtrl)
	// No calls expected: the guard short-circuits before any listing.

	regions := []types.Region{{RegionName: aws.String("us-east-1")}}
	store := newTestPricingStore(regions, pricingClient)

	// Simulate an in-flight populate.
	require.True(t, store.populating.CompareAndSwap(false, true))

	store.Populate(t.Context())

	select {
	case <-store.Done():
		t.Fatal("a skipped populate must not signal readiness")
	default:
	}
}

// TestPricingStore_Populate_ListPricesErrorCountsAndContinues verifies a
// failing region is counted and skipped without dropping its healthy
// siblings.
func TestPricingStore_Populate_ListPricesErrorCountsAndContinues(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	pricingClient := mock.NewMockClient(mockCtrl)
	pricingClient.EXPECT().ListRDSPrices(gomock.Any(), "us-east-1").
		Return([]string{postgresPrice("us-east-1", "0.456")}, nil).
		Times(1)
	pricingClient.EXPECT().ListRDSPrices(gomock.Any(), "eu-west-1").
		Return(nil, errors.New("boom")).
		Times(1)

	regions := []types.Region{
		{RegionName: aws.String("us-east-1")},
		{RegionName: aws.String("eu-west-1")},
	}
	store := newTestPricingStore(regions, pricingClient)

	store.Populate(t.Context())

	_, ok := store.Get(warmKey)
	assert.True(t, ok, "healthy region's price should still be cached")
	assert.Equal(t, 1.0, testutil.ToFloat64(store.populateErrors.WithLabelValues("pricing", "eu-west-1", "list_prices")))
}

// TestPricingStore_Populate_ParseErrorCounts verifies a product that fails to
// parse into a pricing key is counted and skipped.
func TestPricingStore_Populate_ParseErrorCounts(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	pricingClient := mock.NewMockClient(mockCtrl)
	pricingClient.EXPECT().ListRDSPrices(gomock.Any(), gomock.Any()).
		Return([]string{`{invalid`}, nil).
		Times(1)

	regions := []types.Region{{RegionName: aws.String("us-east-1")}}
	store := newTestPricingStore(regions, pricingClient)

	store.Populate(t.Context())

	assert.Equal(t, 1.0, testutil.ToFloat64(store.populateErrors.WithLabelValues("pricing", "us-east-1", "parse_pricing")))
}

// TestPricingStore_Populate_RegionListTimeout verifies the background listing
// is always bounded: a positive RegionListTimeout is honoured, and a zero
// value falls back to the internal safety ceiling rather than running
// unbounded.
func TestPricingStore_Populate_RegionListTimeout(t *testing.T) {
	tests := []struct {
		name              string
		regionListTimeout time.Duration
		wantMaxRemaining  time.Duration
	}{
		{name: "zero falls back to the safety ceiling", regionListTimeout: 0, wantMaxRemaining: defaultPopulateTimeout},
		{name: "positive imposes a tighter deadline", regionListTimeout: 30 * time.Second, wantMaxRemaining: 30 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockCtrl := gomock.NewController(t)
			defer mockCtrl.Finish()

			var deadline time.Time
			var hasDeadline bool
			pricingClient := mock.NewMockClient(mockCtrl)
			pricingClient.EXPECT().ListRDSPrices(gomock.Any(), gomock.Any()).
				DoAndReturn(func(ctx context.Context, _ string) ([]string, error) {
					deadline, hasDeadline = ctx.Deadline()
					return nil, nil
				}).
				Times(1)

			regions := []types.Region{{RegionName: aws.String("us-east-1")}}
			store := newTestPricingStore(regions, pricingClient)
			store.regionListTimeout = tt.regionListTimeout

			// Parent context carries no deadline, so any deadline observed comes
			// from the per-region bound.
			store.Populate(context.Background())
			require.True(t, hasDeadline, "background listing must always be bounded")
			assert.LessOrEqual(t, time.Until(deadline), tt.wantMaxRemaining)
		})
	}
}

// TestPricingStore_Populate_SlowRegionFailsFast verifies a region whose
// listing hangs is bounded by regionListTimeout and does not block healthy
// regions.
func TestPricingStore_Populate_SlowRegionFailsFast(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	pricingClient := mock.NewMockClient(mockCtrl)
	pricingClient.EXPECT().ListRDSPrices(gomock.Any(), "us-east-1").
		Return([]string{postgresPrice("us-east-1", "0.456")}, nil).
		Times(1)
	// The slow region blocks until its timeout-bounded context is cancelled.
	pricingClient.EXPECT().ListRDSPrices(gomock.Any(), "ap-southeast-7").
		DoAndReturn(func(ctx context.Context, _ string) ([]string, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}).
		Times(1)

	regions := []types.Region{
		{RegionName: aws.String("us-east-1")},
		{RegionName: aws.String("ap-southeast-7")},
	}
	store := newTestPricingStore(regions, pricingClient)
	store.regionListTimeout = 50 * time.Millisecond

	start := time.Now()
	store.Populate(t.Context())
	elapsed := time.Since(start)

	assert.Less(t, elapsed, 5*time.Second, "slow region should not block the populate")
	_, ok := store.Get(warmKey)
	assert.True(t, ok, "healthy region's price should still be cached")
}
