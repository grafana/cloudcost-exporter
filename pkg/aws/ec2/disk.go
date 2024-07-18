package ec2

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	ec22 "github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/grafana/cloudcost-exporter/pkg/aws/services/ec2"
)

const eksPVTagName = "kubernetes.io/created-for/pv/name"

func ListEBSVolumes(ctx context.Context, client ec2.EC2) ([]types.Volume, error) {
	params := &ec22.DescribeVolumesInput{
		Filters: []types.Filter{
			// excludes volumes created from snapshots
			{
				Name:   aws.String("snapshot-id"),
				Values: []string{""},
			},
		},
	}

	pager := ec22.NewDescribeVolumesPaginator(client, params)
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
