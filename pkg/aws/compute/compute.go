package compute

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	ec22 "github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/grafana/cloudcost-exporter/pkg/aws/services/ec2"
)

const maxResults = 1000

func ListComputeInstances(ctx context.Context, client ec2.EC2) ([]types.Reservation, error) {
	dii := &ec22.DescribeInstancesInput{
		// 1000 max results was decided arbitrarily. This can likely be tuned.
		MaxResults: aws.Int32(maxResults),
	}
	var instances []types.Reservation
	for {
		resp, err := client.DescribeInstances(ctx, dii)
		if err != nil {
			return nil, err
		}
		instances = append(instances, resp.Reservations...)
		if resp.NextToken == nil || *resp.NextToken == "" {
			break
		}
		dii.NextToken = resp.NextToken
	}

	return instances, nil
}

var clusterTags = []string{"cluster", "eks:cluster-name", "aws:eks:cluster-name"}

func ClusterNameFromInstance(instance types.Instance) string {
	for _, tag := range instance.Tags {
		for _, key := range clusterTags {
			if *tag.Key == key {
				return *tag.Value
			}
		}
	}
	return ""
}
