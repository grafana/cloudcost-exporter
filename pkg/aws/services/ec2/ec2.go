package ec2

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/service/ec2"
)

//go:generate mockgen -source=ec2.go -destination ../mocks/ec2.go -package mocks

type EC2 interface {
	DescribeInstances(ctx context.Context, e *ec2.DescribeInstancesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error)
	DescribeRegions(ctx context.Context, e *ec2.DescribeRegionsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeRegionsOutput, error)
	DescribeSpotPriceHistory(ctx context.Context, input *ec2.DescribeSpotPriceHistoryInput, optFns ...func(*ec2.Options)) (*ec2.DescribeSpotPriceHistoryOutput, error)
	DescribeVolumes(context.Context, *ec2.DescribeVolumesInput, ...func(*ec2.Options)) (*ec2.DescribeVolumesOutput, error)
}
