package pricingstore_test

import (
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	aws "github.com/aws/aws-sdk-go-v2/aws"
	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	pricingTypes "github.com/aws/aws-sdk-go-v2/service/pricing/types"

	awsclient "github.com/grafana/cloudcost-exporter/pkg/aws/client"
	mock_client "github.com/grafana/cloudcost-exporter/pkg/aws/client/mocks"
	"github.com/grafana/cloudcost-exporter/pkg/aws/pricingstore"
)

var testLogger = slog.New(slog.NewTextHandler(os.Stdout, nil))

func TestNewPricingStore(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	tests := map[string]struct {
		logger          *slog.Logger
		regions         []ec2Types.Region
		regionClient    *mock_client.MockClient
		expectedRegions int
	}{
		"creates new pricing store with empty regions": {
			logger:          testLogger,
			regions:         []ec2Types.Region{},
			regionClient:    nil,
			expectedRegions: 0,
		},
		"creates new pricing store with single region": {
			logger: testLogger,
			regions: []ec2Types.Region{
				{RegionName: aws.String("us-east-1")},
			},
			regionClient: func() *mock_client.MockClient {
				m := mock_client.NewMockClient(ctrl)
				m.EXPECT().
					ListEC2ServicePrices(gomock.Any(), "us-east-1", []pricingTypes.Filter{}).
					Return([]string{
						`{"product":{"attributes":{"usagetype":"USE1-NatGateway-Hours","regionCode":"us-east-1"}},"terms":{"OnDemand":{"test":{"priceDimensions":{"test":{"pricePerUnit":{"USD":"0.004"}}}}}}}`,
						`{"product":{"attributes":{"usagetype":"USE1-NatGateway-Bytes","regionCode":"us-east-1"}},"terms":{"OnDemand":{"test":{"priceDimensions":{"test":{"pricePerUnit":{"USD":"0.045"}}}}}}}`,
					}, nil).
					Times(1)
				return m
			}(),
			expectedRegions: 1,
		},
		"creates new pricing store with multiple regions": {
			logger: testLogger,
			regions: []ec2Types.Region{
				{RegionName: aws.String("us-east-1")},
				{RegionName: aws.String("us-west-2")},
			},
			regionClient: func() *mock_client.MockClient {
				m := mock_client.NewMockClient(ctrl)
				m.EXPECT().
					ListEC2ServicePrices(gomock.Any(), "us-east-1", []pricingTypes.Filter{}).
					Return([]string{
						`{"product":{"attributes":{"usagetype":"USE1-NatGateway-Hours","regionCode":"us-east-1"}},"terms":{"OnDemand":{"test":{"priceDimensions":{"test":{"pricePerUnit":{"USD":"0.004"}}}}}}}`,
						`{"product":{"attributes":{"usagetype":"USE1-NatGateway-Bytes","regionCode":"us-east-1"}},"terms":{"OnDemand":{"test":{"priceDimensions":{"test":{"pricePerUnit":{"USD":"0.045"}}}}}}}`,
					}, nil).
					Times(1)
				m.EXPECT().
					ListEC2ServicePrices(gomock.Any(), "us-west-2", []pricingTypes.Filter{}).
					Return([]string{
						`{"product":{"attributes":{"usagetype":"USW2-NatGateway-Hours","regionCode":"us-west-2"}},"terms":{"OnDemand":{"test":{"priceDimensions":{"test":{"pricePerUnit":{"USD":"0.005"}}}}}}}`,
						`{"product":{"attributes":{"usagetype":"USW2-NatGateway-Bytes","regionCode":"us-west-2"}},"terms":{"OnDemand":{"test":{"priceDimensions":{"test":{"pricePerUnit":{"USD":"0.055"}}}}}}}`,
					}, nil).
					Times(1)
				return m
			}(),
			expectedRegions: 2,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			awsRegionClientMap := map[string]awsclient.Client{}
			for i := 0; i < tt.expectedRegions; i++ {
				regionName := *tt.regions[i].RegionName
				awsRegionClientMap[regionName] = tt.regionClient
			}

			store := pricingstore.NewPricingStore(t.Context(), tt.logger, tt.regions, awsRegionClientMap, []pricingTypes.Filter{})

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
