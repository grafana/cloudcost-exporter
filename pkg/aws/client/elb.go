package client

import (
	"context"

	elbTypes "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	elbv2client "github.com/grafana/cloudcost-exporter/pkg/aws/services/elbv2"
)

type elb struct {
	client elbv2client.ELBv2
}

func newELB(client elbv2client.ELBv2) *elb {
	return &elb{client: client}
}

func (e *elb) describeLoadBalancers(ctx context.Context) ([]elbTypes.LoadBalancer, error) {
	o, err := e.client.DescribeLoadBalancers(ctx, nil)
	if err != nil {
		return nil, err
	}
	return o.LoadBalancers, nil
}
