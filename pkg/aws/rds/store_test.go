package rds

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	rdsTypes "github.com/aws/aws-sdk-go-v2/service/rds/types"
	"github.com/grafana/cloudcost-exporter/pkg/aws/client"
	mock "github.com/grafana/cloudcost-exporter/pkg/aws/client/mocks"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// warmKey is the pricing key produced by both instanceFor and postgresPrice.
var warmKey = createPricingKey("us-east-1", "db.t3.medium", "PostgreSQL", "", "Single-AZ", "No license required", "AWS Region")

// newTestStore builds a store without the background goroutine the production
// constructor starts, so tests can drive Populate synchronously.
func newTestStore(regions []types.Region, regionMap map[string]client.Client, pricingClient client.Client, pm *pricingMap) *instanceStore {
	return &instanceStore{
		logger:            slog.Default(),
		regions:           regions,
		regionMap:         regionMap,
		pricingClient:     pricingClient,
		pricingMap:        pm,
		concurrency:       populateConcurrency,
		populateErrors:    newPopulateErrorsCounter(),
		instances:         make(map[string][]rdsTypes.DBInstance),
		initialPopulation: make(chan struct{}),
	}
}

// TestStore_Populate_WarmsInstancesAndPricing verifies a populate caches the
// region's instances and fills the pricing map off the scrape path.
func TestStore_Populate_WarmsInstancesAndPricing(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	regionClient := mock.NewMockClient(mockCtrl)
	regionClient.EXPECT().ListRDSInstances(gomock.Any()).
		Return([]rdsTypes.DBInstance{instanceFor("us-east-1", "db-1")}, nil).
		Times(1)

	pricingClient := mock.NewMockClient(mockCtrl)
	pricingClient.EXPECT().ListRDSPrices(gomock.Any(), gomock.Any()).
		Return([]string{postgresPrice("us-east-1", "0.456")}, nil).
		Times(1)

	pm := newPricingMap()
	regions := []types.Region{{RegionName: aws.String("us-east-1")}}
	regionMap := map[string]client.Client{"us-east-1": regionClient}
	store := newTestStore(regions, regionMap, pricingClient, pm)

	store.Populate(t.Context())

	assert.Len(t, store.Get("us-east-1"), 1)
	price, ok := pm.Get(warmKey)
	assert.True(t, ok, "price should be warmed during populate")
	assert.Equal(t, 0.456, price)
}

// TestStore_Populate_PricingListedOncePerRegion verifies pricing is listed in
// bulk once per region, independent of how many instances the region has.
func TestStore_Populate_PricingListedOncePerRegion(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	regionClient := mock.NewMockClient(mockCtrl)
	regionClient.EXPECT().ListRDSInstances(gomock.Any()).
		Return([]rdsTypes.DBInstance{
			instanceFor("us-east-1", "db-1"),
			instanceFor("us-east-1", "db-2"),
		}, nil).
		Times(1)

	pricingClient := mock.NewMockClient(mockCtrl)
	pricingClient.EXPECT().ListRDSPrices(gomock.Any(), gomock.Any()).
		Return([]string{postgresPrice("us-east-1", "0.456")}, nil).
		Times(1) // one bulk price list regardless of instance count

	pm := newPricingMap()
	regions := []types.Region{{RegionName: aws.String("us-east-1")}}
	regionMap := map[string]client.Client{"us-east-1": regionClient}
	store := newTestStore(regions, regionMap, pricingClient, pm)

	store.Populate(t.Context())
	assert.Len(t, store.Get("us-east-1"), 2)
}

// TestStore_Populate_Refresh verifies a second populate re-lists prices so the
// map tracks the latest rates.
func TestStore_Populate_Refresh(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	regionClient := mock.NewMockClient(mockCtrl)
	regionClient.EXPECT().ListRDSInstances(gomock.Any()).
		Return([]rdsTypes.DBInstance{instanceFor("us-east-1", "db-1")}, nil).
		Times(2)

	pricingClient := mock.NewMockClient(mockCtrl)
	gomock.InOrder(
		pricingClient.EXPECT().ListRDSPrices(gomock.Any(), gomock.Any()).
			Return([]string{postgresPrice("us-east-1", "0.456")}, nil).Times(1),
		pricingClient.EXPECT().ListRDSPrices(gomock.Any(), gomock.Any()).
			Return([]string{postgresPrice("us-east-1", "0.789")}, nil).Times(1),
	)

	pm := newPricingMap()
	regions := []types.Region{{RegionName: aws.String("us-east-1")}}
	regionMap := map[string]client.Client{"us-east-1": regionClient}
	store := newTestStore(regions, regionMap, pricingClient, pm)

	store.Populate(t.Context())
	price, _ := pm.Get(warmKey)
	assert.Equal(t, 0.456, price)

	store.Populate(t.Context())
	price, _ = pm.Get(warmKey)
	assert.Equal(t, 0.789, price, "refresh should overwrite the cached price")
}

// TestStore_Done_ClosesAfterPopulate verifies readiness is signalled once the
// first populate attempt finishes.
func TestStore_Done_ClosesAfterPopulate(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	regionClient := mock.NewMockClient(mockCtrl)
	regionClient.EXPECT().ListRDSInstances(gomock.Any()).
		Return([]rdsTypes.DBInstance{instanceFor("us-east-1", "db-1")}, nil).
		Times(1)
	pricingClient := mock.NewMockClient(mockCtrl)
	expectPricing(pricingClient, "0.456")

	regions := []types.Region{{RegionName: aws.String("us-east-1")}}
	regionMap := map[string]client.Client{"us-east-1": regionClient}
	store := newTestStore(regions, regionMap, pricingClient, newPricingMap())

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

// TestStore_Populate_OverlapGuard verifies a populate is skipped while another
// is still running, so a slow AWS API cannot double the load.
func TestStore_Populate_OverlapGuard(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	regionClient := mock.NewMockClient(mockCtrl)
	// No calls expected: the guard short-circuits before any listing.

	regions := []types.Region{{RegionName: aws.String("us-east-1")}}
	regionMap := map[string]client.Client{"us-east-1": regionClient}
	store := newTestStore(regions, regionMap, regionClient, newPricingMap())

	// Simulate an in-flight populate.
	require.True(t, store.populating.CompareAndSwap(false, true))

	store.Populate(t.Context())

	select {
	case <-store.Done():
		t.Fatal("a skipped populate must not signal readiness")
	default:
	}
}

// TestStore_Populate_ListErrorCountsAndContinues verifies a failing region is
// counted and skipped without dropping its healthy siblings.
func TestStore_Populate_ListErrorCountsAndContinues(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	healthy := mock.NewMockClient(mockCtrl)
	healthy.EXPECT().ListRDSInstances(gomock.Any()).
		Return([]rdsTypes.DBInstance{instanceFor("us-east-1", "healthy")}, nil).
		Times(1)

	broken := mock.NewMockClient(mockCtrl)
	broken.EXPECT().ListRDSInstances(gomock.Any()).
		Return(nil, errors.New("boom")).
		Times(1)

	pricingClient := mock.NewMockClient(mockCtrl)
	expectPricing(pricingClient, "0.456")

	regions := []types.Region{
		{RegionName: aws.String("us-east-1")},
		{RegionName: aws.String("eu-west-1")},
	}
	regionMap := map[string]client.Client{
		"us-east-1": healthy,
		"eu-west-1": broken,
	}
	store := newTestStore(regions, regionMap, pricingClient, newPricingMap())

	store.Populate(t.Context())

	assert.Len(t, store.Get("us-east-1"), 1, "healthy region should still be cached")
	assert.Empty(t, store.Get("eu-west-1"), "failed region should have no cached instances")
	assert.Equal(t, 1.0, testutil.ToFloat64(store.populateErrors.WithLabelValues("instances", "eu-west-1", "list_instances")))
}

// TestStore_Populate_MissingClientCounts verifies a region without a client is
// counted and skipped.
func TestStore_Populate_MissingClientCounts(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	regions := []types.Region{{RegionName: aws.String("us-east-1")}}
	store := newTestStore(regions, map[string]client.Client{}, nil, newPricingMap())

	store.Populate(t.Context())

	assert.Equal(t, 1.0, testutil.ToFloat64(store.populateErrors.WithLabelValues("instances", "us-east-1", "lookup_client")))
}

// TestStore_Populate_RegionListTimeout verifies the background listing is
// always bounded: a positive RegionListTimeout is honoured, and a zero value
// falls back to the internal safety ceiling rather than running unbounded.
func TestStore_Populate_RegionListTimeout(t *testing.T) {
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
			regionClient := mock.NewMockClient(mockCtrl)
			regionClient.EXPECT().ListRDSInstances(gomock.Any()).
				DoAndReturn(func(ctx context.Context) ([]rdsTypes.DBInstance, error) {
					deadline, hasDeadline = ctx.Deadline()
					return nil, nil
				}).
				Times(1)
			regionClient.EXPECT().ListRDSPrices(gomock.Any(), gomock.Any()).
				Return(nil, nil).
				AnyTimes()

			regions := []types.Region{{RegionName: aws.String("us-east-1")}}
			regionMap := map[string]client.Client{"us-east-1": regionClient}
			store := newTestStore(regions, regionMap, regionClient, newPricingMap())
			store.regionListTimeout = tt.regionListTimeout

			// Parent context carries no deadline, so any deadline observed comes
			// from the per-region bound.
			store.Populate(context.Background())
			require.True(t, hasDeadline, "background listing must always be bounded")
			assert.LessOrEqual(t, time.Until(deadline), tt.wantMaxRemaining)
		})
	}
}

// TestStore_Populate_SlowRegionFailsFast verifies a region whose listing hangs
// is bounded by regionListTimeout and does not block healthy regions.
func TestStore_Populate_SlowRegionFailsFast(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	healthy := mock.NewMockClient(mockCtrl)
	healthy.EXPECT().ListRDSInstances(gomock.Any()).
		Return([]rdsTypes.DBInstance{instanceFor("us-east-1", "healthy")}, nil).
		Times(1)

	// The slow region blocks until its timeout-bounded context is cancelled.
	slow := mock.NewMockClient(mockCtrl)
	slow.EXPECT().ListRDSInstances(gomock.Any()).
		DoAndReturn(func(ctx context.Context) ([]rdsTypes.DBInstance, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}).
		Times(1)

	pricingClient := mock.NewMockClient(mockCtrl)
	expectPricing(pricingClient, "0.456")

	regions := []types.Region{
		{RegionName: aws.String("us-east-1")},
		{RegionName: aws.String("ap-southeast-7")},
	}
	regionMap := map[string]client.Client{
		"us-east-1":      healthy,
		"ap-southeast-7": slow,
	}
	store := newTestStore(regions, regionMap, pricingClient, newPricingMap())
	store.regionListTimeout = 50 * time.Millisecond

	start := time.Now()
	store.Populate(t.Context())
	elapsed := time.Since(start)

	assert.Less(t, elapsed, 5*time.Second, "slow region should not block the populate")
	assert.Len(t, store.Get("us-east-1"), 1, "healthy region should still be cached")
	assert.Empty(t, store.Get("ap-southeast-7"), "slow region should be skipped")
}
