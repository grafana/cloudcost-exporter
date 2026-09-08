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

// newTestInstanceStore builds a store without the background goroutine the
// production constructor starts, so tests can drive Populate synchronously.
func newTestInstanceStore(regions []types.Region, regionMap map[string]client.Client) *instanceStore {
	return &instanceStore{
		logger:            slog.Default(),
		regions:           regions,
		regionMap:         regionMap,
		concurrency:       populateConcurrency,
		populateErrors:    newPopulateErrorsCounter(),
		instances:         make(map[string][]rdsTypes.DBInstance),
		initialPopulation: make(chan struct{}),
	}
}

// TestInstanceStore_Populate_WarmsInstances verifies a populate caches the
// region's instances off the scrape path.
func TestInstanceStore_Populate_WarmsInstances(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	regionClient := mock.NewMockClient(mockCtrl)
	regionClient.EXPECT().ListRDSInstances(gomock.Any()).
		Return([]rdsTypes.DBInstance{instanceFor("us-east-1", "db-1")}, nil).
		Times(1)

	regions := []types.Region{{RegionName: aws.String("us-east-1")}}
	regionMap := map[string]client.Client{"us-east-1": regionClient}
	store := newTestInstanceStore(regions, regionMap)

	store.Populate(t.Context())

	assert.Len(t, store.Get("us-east-1"), 1)
}

// TestInstanceStore_Populate_Refresh verifies a second populate re-lists
// instances so the cache tracks the latest inventory.
func TestInstanceStore_Populate_Refresh(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	regionClient := mock.NewMockClient(mockCtrl)
	gomock.InOrder(
		regionClient.EXPECT().ListRDSInstances(gomock.Any()).
			Return([]rdsTypes.DBInstance{instanceFor("us-east-1", "db-1")}, nil).Times(1),
		regionClient.EXPECT().ListRDSInstances(gomock.Any()).
			Return([]rdsTypes.DBInstance{instanceFor("us-east-1", "db-1"), instanceFor("us-east-1", "db-2")}, nil).Times(1),
	)

	regions := []types.Region{{RegionName: aws.String("us-east-1")}}
	regionMap := map[string]client.Client{"us-east-1": regionClient}
	store := newTestInstanceStore(regions, regionMap)

	store.Populate(t.Context())
	assert.Len(t, store.Get("us-east-1"), 1)

	store.Populate(t.Context())
	assert.Len(t, store.Get("us-east-1"), 2, "refresh should overwrite the cached inventory")
}

// TestInstanceStore_Populate_FailedRefreshKeepsStaleData verifies a region
// that previously warmed successfully keeps serving its last-known-good
// inventory when a later refresh for that region fails, rather than going
// empty.
func TestInstanceStore_Populate_FailedRefreshKeepsStaleData(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	regionClient := mock.NewMockClient(mockCtrl)
	gomock.InOrder(
		regionClient.EXPECT().ListRDSInstances(gomock.Any()).
			Return([]rdsTypes.DBInstance{instanceFor("us-east-1", "db-1")}, nil).Times(1),
		regionClient.EXPECT().ListRDSInstances(gomock.Any()).
			Return(nil, errors.New("boom")).Times(1),
	)

	regions := []types.Region{{RegionName: aws.String("us-east-1")}}
	regionMap := map[string]client.Client{"us-east-1": regionClient}
	store := newTestInstanceStore(regions, regionMap)

	store.Populate(t.Context())
	assert.Len(t, store.Get("us-east-1"), 1, "region should warm on the first populate")

	store.Populate(t.Context())
	assert.Len(t, store.Get("us-east-1"), 1, "a failed refresh should keep serving the last-known-good inventory")
	assert.Equal(t, 1.0, testutil.ToFloat64(store.populateErrors.WithLabelValues("instances", "us-east-1", "list_instances")))
}

// TestInstanceStore_Done_ClosesAfterPopulate verifies readiness is signalled
// once the first populate attempt finishes.
func TestInstanceStore_Done_ClosesAfterPopulate(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	regionClient := mock.NewMockClient(mockCtrl)
	regionClient.EXPECT().ListRDSInstances(gomock.Any()).
		Return([]rdsTypes.DBInstance{instanceFor("us-east-1", "db-1")}, nil).
		Times(1)

	regions := []types.Region{{RegionName: aws.String("us-east-1")}}
	regionMap := map[string]client.Client{"us-east-1": regionClient}
	store := newTestInstanceStore(regions, regionMap)

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

// TestInstanceStore_Populate_OverlapGuard verifies a populate is skipped while
// another is still running, so a slow AWS API cannot double the load.
func TestInstanceStore_Populate_OverlapGuard(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	regionClient := mock.NewMockClient(mockCtrl)
	// No calls expected: the guard short-circuits before any listing.

	regions := []types.Region{{RegionName: aws.String("us-east-1")}}
	regionMap := map[string]client.Client{"us-east-1": regionClient}
	store := newTestInstanceStore(regions, regionMap)

	// Simulate an in-flight populate.
	require.True(t, store.populating.CompareAndSwap(false, true))

	store.Populate(t.Context())

	select {
	case <-store.Done():
		t.Fatal("a skipped populate must not signal readiness")
	default:
	}
}

// TestInstanceStore_Populate_ListErrorCountsAndContinues verifies a failing
// region is counted and skipped without dropping its healthy siblings.
func TestInstanceStore_Populate_ListErrorCountsAndContinues(t *testing.T) {
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

	regions := []types.Region{
		{RegionName: aws.String("us-east-1")},
		{RegionName: aws.String("eu-west-1")},
	}
	regionMap := map[string]client.Client{
		"us-east-1": healthy,
		"eu-west-1": broken,
	}
	store := newTestInstanceStore(regions, regionMap)

	store.Populate(t.Context())

	assert.Len(t, store.Get("us-east-1"), 1, "healthy region should still be cached")
	assert.Empty(t, store.Get("eu-west-1"), "failed region should have no cached instances")
	assert.Equal(t, 1.0, testutil.ToFloat64(store.populateErrors.WithLabelValues("instances", "eu-west-1", "list_instances")))
}

// TestInstanceStore_Populate_MissingClientCounts verifies a region without a
// client is counted and skipped.
func TestInstanceStore_Populate_MissingClientCounts(t *testing.T) {
	regions := []types.Region{{RegionName: aws.String("us-east-1")}}
	store := newTestInstanceStore(regions, map[string]client.Client{})

	store.Populate(t.Context())

	assert.Equal(t, 1.0, testutil.ToFloat64(store.populateErrors.WithLabelValues("instances", "us-east-1", "lookup_client")))
}

// TestInstanceStore_Populate_RegionListTimeout verifies the background
// listing is always bounded: a positive RegionListTimeout is honoured, and a
// zero value falls back to the internal safety ceiling rather than running
// unbounded.
func TestInstanceStore_Populate_RegionListTimeout(t *testing.T) {
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

			regions := []types.Region{{RegionName: aws.String("us-east-1")}}
			regionMap := map[string]client.Client{"us-east-1": regionClient}
			store := newTestInstanceStore(regions, regionMap)
			store.regionListTimeout = tt.regionListTimeout

			// Parent context carries no deadline, so any deadline observed comes
			// from the per-region bound.
			store.Populate(context.Background())
			require.True(t, hasDeadline, "background listing must always be bounded")
			assert.LessOrEqual(t, time.Until(deadline), tt.wantMaxRemaining)
		})
	}
}

// TestInstanceStore_Populate_SlowRegionFailsFast verifies a region whose
// listing hangs is bounded by regionListTimeout and does not block healthy
// regions.
func TestInstanceStore_Populate_SlowRegionFailsFast(t *testing.T) {
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

	regions := []types.Region{
		{RegionName: aws.String("us-east-1")},
		{RegionName: aws.String("ap-southeast-7")},
	}
	regionMap := map[string]client.Client{
		"us-east-1":      healthy,
		"ap-southeast-7": slow,
	}
	store := newTestInstanceStore(regions, regionMap)
	store.regionListTimeout = 50 * time.Millisecond

	start := time.Now()
	store.Populate(t.Context())
	elapsed := time.Since(start)

	assert.Less(t, elapsed, 5*time.Second, "slow region should not block the populate")
	assert.Len(t, store.Get("us-east-1"), 1, "healthy region should still be cached")
	assert.Empty(t, store.Get("ap-southeast-7"), "slow region should be skipped")
}
