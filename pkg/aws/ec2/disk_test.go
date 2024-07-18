package ec2

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

func TestListEBSVolumes(t *testing.T) {
	tests := map[string]struct {
		ctx             context.Context
		DescribeVolumes func(ctx context.Context, e *ec2.DescribeVolumesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeVolumesOutput, error)
		err             error
		expected        []types.Volume
		expectedCalls   int
	}{
		"no volumes should return empty": {
			ctx: context.Background(),
			DescribeVolumes: func(ctx context.Context, e *ec2.DescribeVolumesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeVolumesOutput, error) {
				return &ec2.DescribeVolumesOutput{}, nil
			},
			expectedCalls: 1,
		},
		"ensure errors propagate": {
			ctx: context.Background(),
			DescribeVolumes: func(ctx context.Context, e *ec2.DescribeVolumesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeVolumesOutput, error) {
				return nil, assert.AnError
			},
			err:           assert.AnError,
			expectedCalls: 1,
		},
		"returns volumes": {
			ctx: context.Background(),
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
			ctx: context.Background(),
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
			client := ec22.NewEC2(t)
			client.EXPECT().
				DescribeVolumes(mock.Anything, mock.Anything, mock.Anything).
				RunAndReturn(tt.DescribeVolumes).
				Times(tt.expectedCalls)

			resp, err := ListEBSVolumes(tt.ctx, client)
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
