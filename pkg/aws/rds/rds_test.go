package rds

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	rdsTypes "github.com/aws/aws-sdk-go-v2/service/rds/types"
	"github.com/stretchr/testify/assert"
)

func TestIsOutpostsInstance(t *testing.T) {
	tests := []struct {
		name string
		inst rdsTypes.DBInstance
		want string
	}{
		{
			name: "outposts instance type",
			inst: rdsTypes.DBInstance{
				DBSubnetGroup: &rdsTypes.DBSubnetGroup{
					Subnets: []rdsTypes.Subnet{
						{
							SubnetOutpost: &rdsTypes.Outpost{
								Arn: aws.String("some-outpost-arn"),
							},
						},
					},
				},
			},
			want: "AWS Outposts",
		},
		{
			name: "non-outposts instance type",
			inst: rdsTypes.DBInstance{
				DBSubnetGroup: &rdsTypes.DBSubnetGroup{
					Subnets: []rdsTypes.Subnet{
						{
							SubnetOutpost: nil,
						},
					},
				},
			},
			want: "AWS Region",
		},
		{
			name: "non-outposts instance type: DBSubnetGroup empty",
			inst: rdsTypes.DBInstance{
				DBSubnetGroup: &rdsTypes.DBSubnetGroup{},
			},
			want: "AWS Region",
		},
		{
			name: "non-outposts instance type: arn empty",
			inst: rdsTypes.DBInstance{
				DBSubnetGroup: &rdsTypes.DBSubnetGroup{
					Subnets: []rdsTypes.Subnet{
						{
							SubnetOutpost: &rdsTypes.Outpost{},
						},
					},
				},
			},
			want: "AWS Region",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isOutpostsInstance(tt.inst)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMultiOrSingleAZ(t *testing.T) {
	tests := []struct {
		name    string
		multiAZ bool
		want    string
	}{
		{
			name:    "Multi-AZ",
			multiAZ: true,
			want:    "Multi-AZ",
		},
		{
			name:    "Single-AZ",
			multiAZ: false,
			want:    "Single-AZ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := multiOrSingleAZ(tt.multiAZ)
			assert.Equal(t, tt.want, got)
		})
	}
}
