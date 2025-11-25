//go:generate mockgen -source=elbv2.go -destination=mocks/client.go

package elbv2

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
)

type ELBv2 interface {
	DescribeLoadBalancers(ctx context.Context, params *elasticloadbalancingv2.DescribeLoadBalancersInput, optFns ...func(*elasticloadbalancingv2.Options)) (*elasticloadbalancingv2.DescribeLoadBalancersOutput, error)
}
