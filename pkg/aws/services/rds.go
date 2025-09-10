package rds

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/service/rds"
)

type RDS interface {
	DescribeDBInstances(ctx context.Context, e *rds.DescribeDBInstancesInput, optFns ...func(*rds.Options)) (*rds.DescribeDBInstancesOutput, error)
}
