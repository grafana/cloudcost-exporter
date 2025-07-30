package ec2

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
			ctx := context.Background()

			resp, err := ListEBSVolumes(ctx, client)
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
