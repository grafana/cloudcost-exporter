package pricingstore_test

import (
	"context"
	"log/slog"
	"os"
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

			assert.NotNil(t, store)
			assert.NotNil(t, store.GetPricePerUnitPerRegion())
			assert.Len(t, store.GetPricePerUnitPerRegion(), tt.expectedRegions)

			for i := 0; i < tt.expectedRegions; i++ {
				regionName := *tt.regions[i].RegionName
				prices := store.GetPricePerUnitPerRegion()[regionName]
				assert.NotNil(t, prices)

				// It's challenging to not hardcode the region names/order for this test,
				// and it's not worth the additional complexity.
				// What matters is the underlying logic.

				// The first region is us-east-1.
				if i == 0 {
					assert.Equal(t, (*prices)["USE1-NatGateway-Hours"], 0.004)
					assert.Equal(t, (*prices)["USE1-NatGateway-Bytes"], 0.045)
				}

				// The second region is us-west-2.
				if i == 1 {
					assert.Equal(t, (*prices)["USW2-NatGateway-Hours"], 0.005)
					assert.Equal(t, (*prices)["USW2-NatGateway-Bytes"], 0.055)
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

	calls := map[string]int{}
	store := pricingstore.NewPricingStore(t.Context(), testLogger, regions, func(ctx context.Context, region string) ([]string, error) {
		calls[region]++

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
	assert.Equal(t, 0.004, (*store.GetPricePerUnitPerRegion()["us-east-1"])["USE1-NatGateway-Hours"])
	assert.Equal(t, 0.005, (*store.GetPricePerUnitPerRegion()["us-west-2"])["USW2-NatGateway-Hours"])
}
