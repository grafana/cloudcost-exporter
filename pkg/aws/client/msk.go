package client

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	msk "github.com/aws/aws-sdk-go-v2/service/kafka"
	msktypes "github.com/aws/aws-sdk-go-v2/service/kafka/types"
	mskclient "github.com/grafana/cloudcost-exporter/pkg/aws/services/msk"
)

type mskService struct {
	client mskclient.MSK
}

func newMSK(client mskclient.MSK) *mskService {
	return &mskService{client: client}
}

func (m *mskService) listMSKClusters(ctx context.Context) ([]msktypes.Cluster, error) {
	var clusters []msktypes.Cluster
	pager := msk.NewListClustersV2Paginator(m.client, &msk.ListClustersV2Input{
		MaxResults: aws.Int32(100),
	})

	for pager.HasMorePages() {
		o, err := pager.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		clusters = append(clusters, o.ClusterInfoList...)
	}

	return clusters, nil
}
