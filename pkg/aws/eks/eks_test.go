package eks

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/pricing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	mockec2 "github.com/grafana/cloudcost-exporter/mocks/pkg/aws/services/ec2"
	mockpricing "github.com/grafana/cloudcost-exporter/mocks/pkg/aws/services/pricing"
)

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
			client := mockec2.NewEC2(t)
			client.EXPECT().
				DescribeSpotPriceHistory(mock.Anything, mock.Anything, mock.Anything).
				RunAndReturn(tt.DescribeSpotPriceHistory).
				Times(tt.expectedCalls)

			got, err := ListSpotPrices(tt.ctx, client)
			if tt.err != nil {
				assert.Equal(t, tt.err, err)
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestListComputeInstances(t *testing.T) {
	tests := map[string]struct {
		ctx               context.Context
		DescribeInstances func(ctx context.Context, e *ec2.DescribeInstancesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error)
		err               error
		want              []ec2Types.Reservation
		expectedCalls     int
	}{
		"No instance should return nothing": {
			ctx: context.Background(),
			DescribeInstances: func(ctx context.Context, e *ec2.DescribeInstancesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
				return &ec2.DescribeInstancesOutput{}, nil
			},
			err:           nil,
			want:          nil,
			expectedCalls: 1,
		},
		"Single instance should return a single instance": {
			ctx: context.Background(),
			DescribeInstances: func(ctx context.Context, e *ec2.DescribeInstancesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
				return &ec2.DescribeInstancesOutput{
					Reservations: []ec2Types.Reservation{
						{
							Instances: []ec2Types.Instance{
								{
									InstanceId:   aws.String("i-1234567890abcdef0"),
									InstanceType: ec2Types.InstanceTypeA1Xlarge,
								},
							},
						},
					},
				}, nil
			},
			err: nil,
			want: []ec2Types.Reservation{
				{
					Instances: []ec2Types.Instance{
						{
							InstanceId:   aws.String("i-1234567890abcdef0"),
							InstanceType: ec2Types.InstanceTypeA1Xlarge,
						},
					},
				},
			},
		},
		"Ensure errors propagate": {
			ctx: context.Background(),
			DescribeInstances: func(ctx context.Context, e *ec2.DescribeInstancesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
				return nil, assert.AnError
			},
			err:  assert.AnError,
			want: nil,
		},
		"NextToken should return multiple instances": {
			ctx: context.Background(),
			DescribeInstances: func(ctx context.Context, e *ec2.DescribeInstancesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
				if e.NextToken == nil {
					return &ec2.DescribeInstancesOutput{
						NextToken: aws.String("token"),
						Reservations: []ec2Types.Reservation{
							{
								Instances: []ec2Types.Instance{
									{
										InstanceId:   aws.String("i-1234567890abcdef0"),
										InstanceType: ec2Types.InstanceTypeA1Xlarge,
									},
								},
							},
						},
					}, nil
				}
				return &ec2.DescribeInstancesOutput{
					Reservations: []ec2Types.Reservation{
						{
							Instances: []ec2Types.Instance{
								{
									InstanceId:   aws.String("i-1234567890abcdef0"),
									InstanceType: ec2Types.InstanceTypeA1Xlarge,
								},
							},
						},
					},
				}, nil
			},

			err: nil,
			want: []ec2Types.Reservation{
				{
					Instances: []ec2Types.Instance{
						{
							InstanceId:   aws.String("i-1234567890abcdef0"),
							InstanceType: ec2Types.InstanceTypeA1Xlarge,
						},
					},
				},
				{
					Instances: []ec2Types.Instance{
						{
							InstanceId:   aws.String("i-1234567890abcdef0"),
							InstanceType: ec2Types.InstanceTypeA1Xlarge,
						},
					},
				},
			},
			expectedCalls: 2,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			client := mockec2.NewEC2(t)
			client.EXPECT().
				DescribeInstances(mock.Anything, mock.Anything, mock.Anything).
				RunAndReturn(tt.DescribeInstances).
				Times(tt.expectedCalls)

			got, err := ListComputeInstances(tt.ctx, client)
			assert.Equal(t, tt.err, err)
			assert.Equalf(t, tt.want, got, "ListComputeInstances(%v, %v)", tt.ctx, client)
		})
	}
}

func TestCollector_ListOnDemandPrices(t *testing.T) {
	tests := map[string]struct {
		ctx           context.Context
		region        string
		err           error
		GetProducts   func(ctx context.Context, input *pricing.GetProductsInput, optFns ...func(*pricing.Options)) (*pricing.GetProductsOutput, error)
		want          []string
		expectedCalls int
	}{
		"No products should return nothing": {
			ctx:    context.Background(),
			region: "us-east-1",
			err:    nil,
			want:   nil,
			GetProducts: func(ctx context.Context, input *pricing.GetProductsInput, optFns ...func(*pricing.Options)) (*pricing.GetProductsOutput, error) {
				return &pricing.GetProductsOutput{
					PriceList: []string{},
				}, nil
			},
		},
		"Single product should return a single product": {
			ctx:    context.Background(),
			region: "us-east-1",
			err:    nil,
			want: []string{
				"This is definitely an accurate test",
			},
			GetProducts: func(ctx context.Context, input *pricing.GetProductsInput, optFns ...func(*pricing.Options)) (*pricing.GetProductsOutput, error) {
				return &pricing.GetProductsOutput{
					PriceList: []string{
						"This is definitely an accurate test",
					},
				}, nil
			},
		},
		"Ensure errors propagate": {
			ctx:    context.Background(),
			region: "us-east-1",
			err:    assert.AnError,
			want:   nil,
			GetProducts: func(ctx context.Context, input *pricing.GetProductsInput, optFns ...func(*pricing.Options)) (*pricing.GetProductsOutput, error) {
				return nil, assert.AnError
			},
		},
		"NextToken should return multiple products": {
			ctx:    context.Background(),
			region: "us-east-1",
			err:    nil,
			want: []string{
				"This is definitely an accurate test",
				"This is definitely an accurate test",
			},
			GetProducts: func(ctx context.Context, input *pricing.GetProductsInput, optFns ...func(*pricing.Options)) (*pricing.GetProductsOutput, error) {
				if input.NextToken == nil {
					return &pricing.GetProductsOutput{
						NextToken: aws.String("token"),
						PriceList: []string{
							"This is definitely an accurate test",
						},
					}, nil
				}
				return &pricing.GetProductsOutput{
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
			client := mockpricing.NewPricing(t)
			client.EXPECT().
				GetProducts(mock.Anything, mock.Anything, mock.Anything).
				RunAndReturn(tt.GetProducts).
				Times(tt.expectedCalls)
			got, err := ListOnDemandPrices(tt.ctx, tt.region, client)
			if tt.err != nil {
				assert.Equal(t, tt.err, err)
			}
			assert.Equal(t, tt.want, got)
		})
	}
}
