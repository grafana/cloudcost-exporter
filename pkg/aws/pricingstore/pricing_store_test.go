package pricingstore_test

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	aws "github.com/aws/aws-sdk-go-v2/aws"
	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/grafana/cloudcost-exporter/pkg/aws/pricingstore"
)

var testLogger = slog.New(slog.NewTextHandler(os.Stdout, nil))

func TestNewPricingStore(t *testing.T) {
	tests := map[string]struct {
		logger         *slog.Logger
		regions        []ec2Types.Region
		fetchPrices    pricingstore.PriceFetchFunc
		expectedPrices map[string]map[string]float64
	}{
		"creates new pricing store with empty regions": {
			logger:         testLogger,
			regions:        []ec2Types.Region{},
			fetchPrices:    func(context.Context, string) ([]string, error) { return nil, nil },
			expectedPrices: map[string]map[string]float64{},
		},
		"creates new pricing store with single region": {
			logger: testLogger,
			regions: []ec2Types.Region{
				{RegionName: aws.String("us-east-1")},
			},
			fetchPrices: func(_ context.Context, region string) ([]string, error) {
				if region != "us-east-1" {
					return nil, nil
				}
				return []string{
					`{"product":{"attributes":{"usagetype":"USE1-NatGateway-Hours","regionCode":"us-east-1"}},"terms":{"OnDemand":{"test":{"priceDimensions":{"test":{"pricePerUnit":{"USD":"0.004"}}}}}}}`,
					`{"product":{"attributes":{"usagetype":"USE1-NatGateway-Bytes","regionCode":"us-east-1"}},"terms":{"OnDemand":{"test":{"priceDimensions":{"test":{"pricePerUnit":{"USD":"0.045"}}}}}}}`,
				}, nil
			},
			expectedPrices: map[string]map[string]float64{
				"us-east-1": {
					"USE1-NatGateway-Hours": 0.004,
					"USE1-NatGateway-Bytes": 0.045,
				},
			},
		},
		"creates new pricing store with multiple regions": {
			logger: testLogger,
			regions: []ec2Types.Region{
				{RegionName: aws.String("us-east-1")},
				{RegionName: aws.String("us-west-2")},
			},
			fetchPrices: func(_ context.Context, region string) ([]string, error) {
				switch region {
				case "us-east-1":
					return []string{
						`{"product":{"attributes":{"usagetype":"USE1-NatGateway-Hours","regionCode":"us-east-1"}},"terms":{"OnDemand":{"test":{"priceDimensions":{"test":{"pricePerUnit":{"USD":"0.004"}}}}}}}`,
						`{"product":{"attributes":{"usagetype":"USE1-NatGateway-Bytes","regionCode":"us-east-1"}},"terms":{"OnDemand":{"test":{"priceDimensions":{"test":{"pricePerUnit":{"USD":"0.045"}}}}}}}`,
					}, nil
				case "us-west-2":
					return []string{
						`{"product":{"attributes":{"usagetype":"USW2-NatGateway-Hours","regionCode":"us-west-2"}},"terms":{"OnDemand":{"test":{"priceDimensions":{"test":{"pricePerUnit":{"USD":"0.005"}}}}}}}`,
						`{"product":{"attributes":{"usagetype":"USW2-NatGateway-Bytes","regionCode":"us-west-2"}},"terms":{"OnDemand":{"test":{"priceDimensions":{"test":{"pricePerUnit":{"USD":"0.055"}}}}}}}`,
					}, nil
				default:
					return nil, nil
				}
			},
			expectedPrices: map[string]map[string]float64{
				"us-east-1": {
					"USE1-NatGateway-Hours": 0.004,
					"USE1-NatGateway-Bytes": 0.045,
				},
				"us-west-2": {
					"USW2-NatGateway-Hours": 0.005,
					"USW2-NatGateway-Bytes": 0.055,
				},
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			store, err := pricingstore.NewPricingStore(t.Context(), tt.logger, tt.regions, tt.fetchPrices)

			require.NoError(t, err)
			assert.NotNil(t, store)
			assert.Equal(t, tt.expectedPrices, snapshotToMap(store.Snapshot()))
		})
	}
}

func TestNewPricingStoreInvokesInjectedFetcher(t *testing.T) {
	regions := []ec2Types.Region{
		{RegionName: aws.String("us-east-1")},
		{RegionName: aws.String("us-west-2")},
	}

	var mu sync.Mutex
	calls := map[string]int{}
	store, err := pricingstore.NewPricingStore(t.Context(), testLogger, regions, func(ctx context.Context, region string) ([]string, error) {
		mu.Lock()
		calls[region]++
		mu.Unlock()

		switch region {
		case "us-east-1":
			return []string{
				`{"product":{"attributes":{"usagetype":"USE1-NatGateway-Hours","regionCode":"us-east-1"}},"terms":{"OnDemand":{"test":{"priceDimensions":{"test":{"pricePerUnit":{"USD":"0.004"}}}}}}}`,
			}, nil
		case "us-west-2":
			return []string{
				`{"product":{"attributes":{"usagetype":"USW2-NatGateway-Hours","regionCode":"us-west-2"}},"terms":{"OnDemand":{"test":{"priceDimensions":{"test":{"pricePerUnit":{"USD":"0.005"}}}}}}}`,
			}, nil
		default:
			return nil, nil
		}
	})

	require.NoError(t, err)
	assert.NotNil(t, store)
	assert.Equal(t, map[string]int{
		"us-east-1": 1,
		"us-west-2": 1,
	}, calls)

	assertSnapshotPrice(t, store.Snapshot(), "us-east-1", "USE1-NatGateway-Hours", 0.004)
	assertSnapshotPrice(t, store.Snapshot(), "us-west-2", "USW2-NatGateway-Hours", 0.005)
}

func TestPopulatePricingMapPublishesNewSnapshotWithoutMutatingExistingOne(t *testing.T) {
	regions := []ec2Types.Region{{RegionName: aws.String("us-east-1")}}

	price := `{"product":{"attributes":{"usagetype":"USE1-NatGateway-Hours","regionCode":"us-east-1"}},"terms":{"OnDemand":{"test":{"priceDimensions":{"test":{"pricePerUnit":{"USD":"%s"}}}}}}}`
	currentPrice := "0.004"

	store, err := pricingstore.NewPricingStore(t.Context(), testLogger, regions, func(context.Context, string) ([]string, error) {
		return []string{fmt.Sprintf(price, currentPrice)}, nil
	})
	require.NoError(t, err)

	before := store.Snapshot()

	currentPrice = "0.005"
	assert.NoError(t, store.PopulatePricingMap(t.Context()))

	assertSnapshotPrice(t, before, "us-east-1", "USE1-NatGateway-Hours", 0.004)
	assertSnapshotPrice(t, store.Snapshot(), "us-east-1", "USE1-NatGateway-Hours", 0.005)
}

func TestNewPricingStore_ReturnsErrorWhenFetchFails(t *testing.T) {
	regions := []ec2Types.Region{{RegionName: aws.String("us-east-1")}}
	fetchErr := fmt.Errorf("pricing API unavailable")

	store, err := pricingstore.NewPricingStore(t.Context(), testLogger, regions, func(context.Context, string) ([]string, error) {
		return nil, fetchErr
	})

	assert.Nil(t, store)
	assert.Error(t, err)
}

func snapshotToMap(snapshot pricingstore.Snapshot) map[string]map[string]float64 {
	pricesByRegion := make(map[string]map[string]float64)
	for region, prices := range snapshot.Regions() {
		regionPrices := make(map[string]float64)
		for usageType, price := range prices.Entries() {
			regionPrices[usageType] = price
		}
		pricesByRegion[region] = regionPrices
	}

	return pricesByRegion
}

func assertSnapshotPrice(t *testing.T, snapshot pricingstore.Snapshot, region, usageType string, want float64) {
	t.Helper()

	regionSnapshot, ok := snapshot.Region(region)
	assert.True(t, ok)

	got, ok := regionSnapshot.Get(usageType)
	assert.True(t, ok)
	assert.Equal(t, want, got)
}
