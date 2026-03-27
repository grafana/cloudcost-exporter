package pricingstore_test

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"

	aws "github.com/aws/aws-sdk-go-v2/aws"
	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/grafana/cloudcost-exporter/pkg/aws/pricingstore"
)

var testLogger = slog.New(slog.NewTextHandler(os.Stdout, nil))

func TestNewPricingStore(t *testing.T) {
	tests := map[string]struct {
		logger          *slog.Logger
		regions         []ec2Types.Region
		fetchPrices     pricingstore.PriceFetchFunc
		expectedRegions int
	}{
		"creates new pricing store with empty regions": {
			logger:          testLogger,
			regions:         []ec2Types.Region{},
			fetchPrices:     func(context.Context, string) ([]string, error) { return nil, nil },
			expectedRegions: 0,
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
			expectedRegions: 1,
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
			expectedRegions: 2,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			store := pricingstore.NewPricingStore(t.Context(), tt.logger, tt.regions, tt.fetchPrices)
			snapshot := store.Snapshot()
			pricesByRegion := snapshotToMap(snapshot)

			assert.NotNil(t, store)
			assert.Len(t, pricesByRegion, tt.expectedRegions)

			for i := 0; i < tt.expectedRegions; i++ {
				regionName := *tt.regions[i].RegionName
				prices, ok := pricesByRegion[regionName]
				assert.True(t, ok)

				switch regionName {
				case "us-east-1":
					assert.Equal(t, 0.004, prices["USE1-NatGateway-Hours"])
					assert.Equal(t, 0.045, prices["USE1-NatGateway-Bytes"])
				case "us-west-2":
					assert.Equal(t, 0.005, prices["USW2-NatGateway-Hours"])
					assert.Equal(t, 0.055, prices["USW2-NatGateway-Bytes"])
				}
			}
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
	store := pricingstore.NewPricingStore(t.Context(), testLogger, regions, func(ctx context.Context, region string) ([]string, error) {
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

	assert.NotNil(t, store)
	assert.Equal(t, map[string]int{
		"us-east-1": 1,
		"us-west-2": 1,
	}, calls)

	east, ok := store.Snapshot().Region("us-east-1")
	assert.True(t, ok)
	eastPrice, ok := east.Get("USE1-NatGateway-Hours")
	assert.True(t, ok)
	assert.Equal(t, 0.004, eastPrice)

	west, ok := store.Snapshot().Region("us-west-2")
	assert.True(t, ok)
	westPrice, ok := west.Get("USW2-NatGateway-Hours")
	assert.True(t, ok)
	assert.Equal(t, 0.005, westPrice)
}

func TestPopulatePricingMapPublishesNewSnapshotWithoutMutatingExistingOne(t *testing.T) {
	regions := []ec2Types.Region{{RegionName: aws.String("us-east-1")}}

	price := `{"product":{"attributes":{"usagetype":"USE1-NatGateway-Hours","regionCode":"us-east-1"}},"terms":{"OnDemand":{"test":{"priceDimensions":{"test":{"pricePerUnit":{"USD":"%s"}}}}}}}`
	currentPrice := "0.004"

	store := pricingstore.NewPricingStore(t.Context(), testLogger, regions, func(context.Context, string) ([]string, error) {
		return []string{fmt.Sprintf(price, currentPrice)}, nil
	})

	before := store.Snapshot()

	currentPrice = "0.005"
	assert.NoError(t, store.PopulatePricingMap(t.Context()))

	after := store.Snapshot()

	beforeRegion, ok := before.Region("us-east-1")
	assert.True(t, ok)
	beforePrice, ok := beforeRegion.Get("USE1-NatGateway-Hours")
	assert.True(t, ok)
	assert.Equal(t, 0.004, beforePrice)

	afterRegion, ok := after.Region("us-east-1")
	assert.True(t, ok)
	afterPrice, ok := afterRegion.Get("USE1-NatGateway-Hours")
	assert.True(t, ok)
	assert.Equal(t, 0.005, afterPrice)
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
