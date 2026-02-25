package client

import (
	"context"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsEc2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/cloudcost-exporter/pkg/aws/services/ec2"
)

const maxResults = 1000
const eksPVTagName = "kubernetes.io/created-for/pv/name"
const maxConcurrentAZFetches = 5

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

// runningFilter includes only instances incurring compute costs.
// "stopping" is included to cover hibernated instances, which are billed during that transition.
var runningFilter = types.Filter{
	Name:   aws.String("instance-state-name"),
	Values: []string{"running", "stopping"},
}

func (e *compute) listComputeInstances(ctx context.Context) ([]types.Reservation, error) {
	azs, err := e.getAvailabilityZones(ctx)
	if err != nil {
		return e.listComputeInstancesSequential(ctx, []types.Filter{runningFilter})
	}

	if len(azs) <= 1 {
		return e.listComputeInstancesSequential(ctx, []types.Filter{runningFilter})
	}

	// Fetch instances from each AZ in parallel to improve performance
	var mu sync.Mutex
	var allInstances []types.Reservation

	eg, egCtx := errgroup.WithContext(ctx)
	eg.SetLimit(maxConcurrentAZFetches)

	for _, az := range azs {
		az := az
		eg.Go(func() error {
			filter := []types.Filter{
				runningFilter,
				{
					Name:   aws.String("availability-zone"),
					Values: []string{az},
				},
			}
			instances, err := e.listComputeInstancesSequential(egCtx, filter)
			if err != nil {
				return err
			}

			mu.Lock()
			allInstances = append(allInstances, instances...)
			mu.Unlock()
			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return nil, err
	}

	return allInstances, nil
}

func (e *compute) listComputeInstancesSequential(ctx context.Context, filters []types.Filter) ([]types.Reservation, error) {
	dii := &awsEc2.DescribeInstancesInput{
		MaxResults: aws.Int32(maxResults),
	}
	if len(filters) > 0 {
		dii.Filters = filters
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

func (e *compute) getAvailabilityZones(ctx context.Context) ([]string, error) {
	resp, err := e.client.DescribeAvailabilityZones(ctx, &awsEc2.DescribeAvailabilityZonesInput{
		Filters: []types.Filter{
			{
				Name:   aws.String("state"),
				Values: []string{"available"},
			},
		},
	})
	if err != nil {
		return nil, err
	}

	azs := make([]string, 0, len(resp.AvailabilityZones))
	for _, az := range resp.AvailabilityZones {
		if az.ZoneName != nil {
			azs = append(azs, *az.ZoneName)
		}
	}

	return azs, nil
}

// DISK

func (e *compute) listEBSVolumes(ctx context.Context) ([]types.Volume, error) {
	pager := awsEc2.NewDescribeVolumesPaginator(e.client, &awsEc2.DescribeVolumesInput{})
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
