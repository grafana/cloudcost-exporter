package client

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsEc2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/grafana/cloudcost-exporter/pkg/aws/services/ec2"
)

const maxResults = 1000
const eksPVTagName = "kubernetes.io/created-for/pv/name"

var clusterTags = []string{"cluster", "eks:cluster-name", "aws:eks:cluster-name"}

type compute struct {
	client ec2.EC2
}

func newCompute(client ec2.EC2) *compute {
	return &compute{client: client}
}

func (e *compute) describeRegions(ctx context.Context, allRegions bool) ([]types.Region, error) {
	regions, err := e.client.DescribeRegions(ctx, &awsEc2.DescribeRegionsInput{AllRegions: aws.Bool(allRegions)})
	if err != nil {
		return nil, err
	}

	return regions.Regions, nil
}

func (e *compute) listComputeInstances(ctx context.Context) ([]types.Reservation, error) {
	dii := &awsEc2.DescribeInstancesInput{
		// 1000 max results was decided arbitrarily. This can likely be tuned.
		MaxResults: aws.Int32(maxResults),
	}
	var instances []types.Reservation
	for {
		resp, err := e.client.DescribeInstances(ctx, dii)
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

// DISK

func (e *compute) listEBSVolumes(ctx context.Context) ([]types.Volume, error) {
	params := &awsEc2.DescribeVolumesInput{
		Filters: []types.Filter{
			// excludes volumes created from snapshots
			{
				Name:   aws.String("snapshot-id"),
				Values: []string{""},
			},
		},
	}

	pager := awsEc2.NewDescribeVolumesPaginator(e.client, params)
	var volumes []types.Volume

	for pager.HasMorePages() {
		resp, err := pager.NextPage(ctx)
		if err != nil {
			return nil, err
		}

		volumes = append(volumes, resp.Volumes...)
		if resp.NextToken == nil || *resp.NextToken == "" {
			break
		}
	}

	return volumes, nil
}

func NameFromVolume(volume types.Volume) string {
	for _, tag := range volume.Tags {
		if *tag.Key == eksPVTagName {
			return *tag.Value
		}
	}

	return ""
}

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
