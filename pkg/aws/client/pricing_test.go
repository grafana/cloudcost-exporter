package client

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	awsPricing "github.com/aws/aws-sdk-go-v2/service/pricing"
	"github.com/grafana/cloudcost-exporter/pkg/aws/services/mocks"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

func Test_ListOnDemandPrices(t *testing.T) {
	tests := map[string]struct {
		ctx           context.Context
		region        string
		err           error
		GetProducts   func(ctx context.Context, input *awsPricing.GetProductsInput, optFns ...func(*awsPricing.Options)) (*awsPricing.GetProductsOutput, error)
		want          []string
		expectedCalls int
	}{
		"No products should return nothing": {
			ctx:    context.Background(),
			region: "us-east-1",
			err:    nil,
			want:   nil,
			GetProducts: func(ctx context.Context, input *awsPricing.GetProductsInput, optFns ...func(*awsPricing.Options)) (*awsPricing.GetProductsOutput, error) {
				return &awsPricing.GetProductsOutput{
					PriceList: []string{},
				}, nil
			},
			expectedCalls: 1,
		},
		"Single product should return a single product": {
			ctx:    context.Background(),
			region: "us-east-1",
			err:    nil,
			want: []string{
				"This is definitely an accurate test",
			},
			GetProducts: func(ctx context.Context, input *awsPricing.GetProductsInput, optFns ...func(*awsPricing.Options)) (*awsPricing.GetProductsOutput, error) {
				return &awsPricing.GetProductsOutput{
					PriceList: []string{
						"This is definitely an accurate test",
					},
				}, nil
			},
			expectedCalls: 1,
		},
		"Ensure errors propagate": {
			ctx:    context.Background(),
			region: "us-east-1",
			err:    assert.AnError,
			want:   nil,
			GetProducts: func(ctx context.Context, input *awsPricing.GetProductsInput, optFns ...func(*awsPricing.Options)) (*awsPricing.GetProductsOutput, error) {
				return nil, assert.AnError
			},
			expectedCalls: 1,
		},
		"NextToken should return multiple products": {
			ctx:    context.Background(),
			region: "us-east-1",
			err:    nil,
			want: []string{
				"This is definitely an accurate test",
				"This is definitely an accurate test",
			},
			GetProducts: func(ctx context.Context, input *awsPricing.GetProductsInput, optFns ...func(*awsPricing.Options)) (*awsPricing.GetProductsOutput, error) {
				if input.NextToken == nil {
					return &awsPricing.GetProductsOutput{
						NextToken: aws.String("token"),
						PriceList: []string{
							"This is definitely an accurate test",
						},
					}, nil
				}
				return &awsPricing.GetProductsOutput{
					PriceList: []string{
						"This is definitely an accurate test",
					},
				}, nil
			},
			expectedCalls: 2,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			client := mocks.NewMockPricing(ctrl)
			client.EXPECT().
				GetProducts(gomock.Any(), gomock.Any(), gomock.Any()).
				DoAndReturn(tt.GetProducts).
				Times(tt.expectedCalls)
			c := newPricing(client, nil)
			got, err := c.listOnDemandPrices(tt.ctx, tt.region)
			if tt.err != nil {
				assert.Equal(t, tt.err, err)
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestListSpotPrices(t *testing.T) {
	tests := map[string]struct {
		ctx                      context.Context
		DescribeSpotPriceHistory func(ctx context.Context, input *ec2.DescribeSpotPriceHistoryInput, optFns ...func(options *ec2.Options)) (*ec2.DescribeSpotPriceHistoryOutput, error)
		err                      error
		want                     []ec2Types.SpotPrice
		expectedCalls            int
	}{
		"No instance should return nothing": {
			ctx: context.Background(),
			DescribeSpotPriceHistory: func(ctx context.Context, input *ec2.DescribeSpotPriceHistoryInput, optFns ...func(options *ec2.Options)) (*ec2.DescribeSpotPriceHistoryOutput, error) {
				return &ec2.DescribeSpotPriceHistoryOutput{}, nil
			},
			err:           nil,
			want:          nil,
			expectedCalls: 1,
		},
		"Single instance should return a single instance": {
			ctx: context.Background(),
			DescribeSpotPriceHistory: func(ctx context.Context, input *ec2.DescribeSpotPriceHistoryInput, optFns ...func(options *ec2.Options)) (*ec2.DescribeSpotPriceHistoryOutput, error) {
				return &ec2.DescribeSpotPriceHistoryOutput{
					SpotPriceHistory: []ec2Types.SpotPrice{
						{
							AvailabilityZone: aws.String("us-east-1a"),
							InstanceType:     ec2Types.InstanceTypeC5ad2xlarge,
							SpotPrice:        aws.String("0.4680000000"),
						},
					},
				}, nil
			},
			err: nil,
			want: []ec2Types.SpotPrice{
				{
					AvailabilityZone: aws.String("us-east-1a"),
					InstanceType:     ec2Types.InstanceTypeC5ad2xlarge,
					SpotPrice:        aws.String("0.4680000000"),
				},
			},
			expectedCalls: 1,
		},
		"Ensure errors propagate": {
			ctx: context.Background(),
			DescribeSpotPriceHistory: func(ctx context.Context, input *ec2.DescribeSpotPriceHistoryInput, optFns ...func(options *ec2.Options)) (*ec2.DescribeSpotPriceHistoryOutput, error) {
				return nil, assert.AnError
			},
			err:           assert.AnError,
			want:          nil,
			expectedCalls: 1,
		},
		"NextToken should return multiple instances": {
			ctx: context.Background(),
			DescribeSpotPriceHistory: func(ctx context.Context, input *ec2.DescribeSpotPriceHistoryInput, optFns ...func(options *ec2.Options)) (*ec2.DescribeSpotPriceHistoryOutput, error) {
				if input.NextToken == nil {
					return &ec2.DescribeSpotPriceHistoryOutput{
						NextToken: aws.String("token"),
						SpotPriceHistory: []ec2Types.SpotPrice{
							{
								AvailabilityZone: aws.String("us-east-1a"),
								InstanceType:     ec2Types.InstanceTypeC5ad2xlarge,
								SpotPrice:        aws.String("0.4680000000"),
							},
						},
					}, nil
				}
				return &ec2.DescribeSpotPriceHistoryOutput{
					SpotPriceHistory: []ec2Types.SpotPrice{
						{
							AvailabilityZone: aws.String("us-east-1a"),
							InstanceType:     ec2Types.InstanceTypeC5ad2xlarge,
							SpotPrice:        aws.String("0.4680000000"),
						},
					},
				}, nil
			},
			err: nil,
			want: []ec2Types.SpotPrice{
				{
					AvailabilityZone: aws.String("us-east-1a"),
					InstanceType:     ec2Types.InstanceTypeC5ad2xlarge,
					SpotPrice:        aws.String("0.4680000000"),
				},
				{
					AvailabilityZone: aws.String("us-east-1a"),
					InstanceType:     ec2Types.InstanceTypeC5ad2xlarge,
					SpotPrice:        aws.String("0.4680000000"),
				},
			},
			expectedCalls: 2,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			client := mocks.NewMockEC2(ctrl)
			client.EXPECT().
				DescribeSpotPriceHistory(gomock.Any(), gomock.Any(), gomock.Any()).
				DoAndReturn(tt.DescribeSpotPriceHistory).
				Times(tt.expectedCalls)

			c := newPricing(nil, client)
			got, err := c.listSpotPrices(tt.ctx)
			if tt.err != nil {
				assert.Equal(t, tt.err, err)
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestListStoragePrices(t *testing.T) {
	tests := map[string]struct {
		ctx           context.Context
		region        string
		GetProducts   func(ctx context.Context, input *awsPricing.GetProductsInput, optFns ...func(*awsPricing.Options)) (*awsPricing.GetProductsOutput, error)
		expected      []string
		expectedCalls int
		err           error
	}{
		"Ensure errors propagate": {
			ctx:      context.Background(),
			region:   "us-east-1",
			err:      assert.AnError,
			expected: nil,
			GetProducts: func(ctx context.Context, input *awsPricing.GetProductsInput, optFns ...func(*awsPricing.Options)) (*awsPricing.GetProductsOutput, error) {
				return nil, assert.AnError
			},
			expectedCalls: 1,
		},
		"No volume prices for that region should return empty": {
			ctx:    context.Background(),
			region: "us-east-1",
			GetProducts: func(ctx context.Context, input *awsPricing.GetProductsInput, optFns ...func(*awsPricing.Options)) (*awsPricing.GetProductsOutput, error) {
				return &awsPricing.GetProductsOutput{
					PriceList: []string{},
				}, nil
			},
			expectedCalls: 1,
		},
		"Single product should return a single product": {
			ctx:    context.Background(),
			region: "us-east-1",
			expected: []string{
				"product 1 json response",
			},
			GetProducts: func(ctx context.Context, input *awsPricing.GetProductsInput, optFns ...func(*awsPricing.Options)) (*awsPricing.GetProductsOutput, error) {
				return &awsPricing.GetProductsOutput{
					PriceList: []string{
						"product 1 json response",
					},
				}, nil
			},
			expectedCalls: 1,
		},
		"multiple products should return same length array": {
			ctx:    context.Background(),
			region: "us-east-1",
			err:    nil,
			expected: []string{
				"product 1 json response",
				"product 2 json response",
			},
			GetProducts: func(ctx context.Context, input *awsPricing.GetProductsInput, optFns ...func(*awsPricing.Options)) (*awsPricing.GetProductsOutput, error) {
				if input.NextToken == nil {
					return &awsPricing.GetProductsOutput{
						NextToken: aws.String("token"),
						PriceList: []string{
							"product 1 json response",
						},
					}, nil
				}
				return &awsPricing.GetProductsOutput{
					PriceList: []string{
						"product 2 json response",
					},
				}, nil
			},
			expectedCalls: 2,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			client := mocks.NewMockPricing(ctrl)
			client.EXPECT().
				GetProducts(gomock.Any(), gomock.Any(), gomock.Any()).
				DoAndReturn(tt.GetProducts).
				Times(tt.expectedCalls)

			c := newPricing(client, nil)
			resp, err := c.listStoragePrices(tt.ctx, tt.region)
			if tt.err != nil {
				assert.Equal(t, tt.err, err)
			}
			assert.Equal(t, tt.expected, resp)
		})
	}
}
