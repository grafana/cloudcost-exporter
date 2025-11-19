package client

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/grafana/cloudcost-exporter/pkg/aws/services/mocks"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
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
			ctx: t.Context(),
			DescribeInstances: func(ctx context.Context, e *ec2.DescribeInstancesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
				return &ec2.DescribeInstancesOutput{}, nil
			},
			err:           nil,
			want:          nil,
			expectedCalls: 1,
		},
		"Single instance should return a single instance": {
			ctx: t.Context(),
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
			expectedCalls: 1,
		},
		"Ensure errors propagate": {
			ctx: t.Context(),
			DescribeInstances: func(ctx context.Context, e *ec2.DescribeInstancesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
				return nil, assert.AnError
			},
			err:           assert.AnError,
			want:          nil,
			expectedCalls: 1,
		},
		"NextToken should return multiple instances": {
			ctx: t.Context(),
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
			ctrl := gomock.NewController(t)
			client := mocks.NewMockEC2(ctrl)
			client.EXPECT().
				DescribeInstances(gomock.Any(), gomock.Any(), gomock.Any()).
				DoAndReturn(tt.DescribeInstances).
				Times(tt.expectedCalls)

			c := newCompute(client)
			got, err := c.listComputeInstances(tt.ctx)
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

func TestListEBSVolumes(t *testing.T) {
	tests := map[string]struct {
		DescribeVolumes func(ctx context.Context, e *ec2.DescribeVolumesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeVolumesOutput, error)
		err             error
		expected        []types.Volume
		expectedCalls   int
	}{
		"no volumes should return empty": {
			DescribeVolumes: func(ctx context.Context, e *ec2.DescribeVolumesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeVolumesOutput, error) {
				return &ec2.DescribeVolumesOutput{}, nil
			},
			expectedCalls: 1,
		},
		"ensure errors propagate": {
			DescribeVolumes: func(ctx context.Context, e *ec2.DescribeVolumesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeVolumesOutput, error) {
				return nil, assert.AnError
			},
			err:           assert.AnError,
			expectedCalls: 1,
		},
		"returns volumes": {
			DescribeVolumes: func(ctx context.Context, e *ec2.DescribeVolumesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeVolumesOutput, error) {
				return &ec2.DescribeVolumesOutput{
					Volumes: []types.Volume{
						{
							VolumeId: aws.String("vol-111111111"),
						},
					},
				}, nil
			},
			expected: []types.Volume{
				{
					VolumeId: aws.String("vol-111111111"),
				},
			},
			expectedCalls: 1,
		},
		"paginator iterates over pages": {
			DescribeVolumes: func(ctx context.Context, e *ec2.DescribeVolumesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeVolumesOutput, error) {
				if e.NextToken == nil {
					return &ec2.DescribeVolumesOutput{
						NextToken: aws.String("token"),
						Volumes: []types.Volume{
							{
								VolumeId: aws.String("vol-111111111"),
							},
						},
					}, nil
				}
				return &ec2.DescribeVolumesOutput{
					Volumes: []types.Volume{
						{
							VolumeId: aws.String("vol-2222222222"),
						},
					},
				}, nil
			},
			expected: []types.Volume{
				{
					VolumeId: aws.String("vol-111111111"),
				},
				{
					VolumeId: aws.String("vol-2222222222"),
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
				DescribeVolumes(gomock.Any(), gomock.Any(), gomock.Any()).
				DoAndReturn(tt.DescribeVolumes).
				Times(tt.expectedCalls)
			ctx := t.Context()

			c := newCompute(client)
			resp, err := c.listEBSVolumes(ctx)
			assert.Equal(t, tt.err, err)
			assert.Equal(t, tt.expected, resp)
		})
	}
}

func TestNameFromVolume(t *testing.T) {
	tests := map[string]struct {
		volume   types.Volume
		expected string
	}{
		"no tags returns empty string": {
			volume:   types.Volume{},
			expected: "",
		},
		"tags exist but not the pv name one": {
			volume: types.Volume{
				Tags: []types.Tag{
					{
						Key:   aws.String("asdf"),
						Value: aws.String("asdf"),
					},
				},
			},
			expected: "",
		},
		"tags slice contains the tag for the pv name eks adds to PVs": {
			volume: types.Volume{
				Tags: []types.Tag{
					{
						Key:   aws.String(eksPVTagName),
						Value: aws.String("pvc-1234567890"),
					},
				},
			},
			expected: "pvc-1234567890",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.expected, NameFromVolume(tt.volume))
		})
	}
}
