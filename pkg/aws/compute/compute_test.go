package compute

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	ec22 "github.com/grafana/cloudcost-exporter/mocks/pkg/aws/services/ec2"
)

func TestListComputeInstances(t *testing.T) {
	tests := map[string]struct {
		ctx               context.Context
		DescribeInstances func(ctx context.Context, e *ec2.DescribeInstancesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error)
		err               error
		want              []types.Reservation
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
					Reservations: []types.Reservation{
						{
							Instances: []types.Instance{
								{
									InstanceId:   aws.String("i-1234567890abcdef0"),
									InstanceType: types.InstanceTypeA1Xlarge,
								},
							},
						},
					},
				}, nil
			},
			err: nil,
			want: []types.Reservation{
				{
					Instances: []types.Instance{
						{
							InstanceId:   aws.String("i-1234567890abcdef0"),
							InstanceType: types.InstanceTypeA1Xlarge,
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
						Reservations: []types.Reservation{
							{
								Instances: []types.Instance{
									{
										InstanceId:   aws.String("i-1234567890abcdef0"),
										InstanceType: types.InstanceTypeA1Xlarge,
									},
								},
							},
						},
					}, nil
				}
				return &ec2.DescribeInstancesOutput{
					Reservations: []types.Reservation{
						{
							Instances: []types.Instance{
								{
									InstanceId:   aws.String("i-1234567890abcdef0"),
									InstanceType: types.InstanceTypeA1Xlarge,
								},
							},
						},
					},
				}, nil
			},

			err: nil,
			want: []types.Reservation{
				{
					Instances: []types.Instance{
						{
							InstanceId:   aws.String("i-1234567890abcdef0"),
							InstanceType: types.InstanceTypeA1Xlarge,
						},
					},
				},
				{
					Instances: []types.Instance{
						{
							InstanceId:   aws.String("i-1234567890abcdef0"),
							InstanceType: types.InstanceTypeA1Xlarge,
						},
					},
				},
			},
			expectedCalls: 2,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			client := ec22.NewEC2(t)
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

func Test_clusterNameFromInstance(t *testing.T) {
	tests := map[string]struct {
		instance types.Instance
		want     string
	}{
		"Instance with no tags should return an empty string": {
			instance: types.Instance{},
			want:     "",
		},
		"Instance with a tag should return the cluster name": {
			instance: types.Instance{
				Tags: []types.Tag{
					{
						Key:   aws.String("cluster"),
						Value: aws.String("cluster-name"),
					},
				},
			},
			want: "cluster-name",
		},
		"Instance with eks:clustername should return the cluster name": {
			instance: types.Instance{
				Tags: []types.Tag{
					{
						Key:   aws.String("eks:cluster-name"),
						Value: aws.String("cluster-name"),
					},
				},
			},
			want: "cluster-name",
		},
		"Instance with aws:eks:cluster-name should return the cluster name": {
			instance: types.Instance{
				Tags: []types.Tag{
					{
						Key:   aws.String("eks:cluster-name"),
						Value: aws.String("cluster-name"),
					},
				},
			},
			want: "cluster-name",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equalf(t, tt.want, ClusterNameFromInstance(tt.instance), "ClusterNameFromInstance(%v)", tt.instance)
		})
	}
}
