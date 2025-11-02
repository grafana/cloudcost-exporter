package client

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	rdsTypes "github.com/aws/aws-sdk-go-v2/service/rds/types"
)

type rdsService struct {
	client *rds.Client
}

func newRDS(client *rds.Client) *rdsService {
	return &rdsService{client: client}
}

func (e *rdsService) listRDSInstances(ctx context.Context) ([]rdsTypes.DBInstance, error) {
	var rdsInstances []rdsTypes.DBInstance
	pager := rds.NewDescribeDBInstancesPaginator(e.client, &rds.DescribeDBInstancesInput{
		MaxRecords: aws.Int32(100),
	})

	for pager.HasMorePages() {
		o, err := pager.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		rdsInstances = append(rdsInstances, o.DBInstances...)
	}

	return rdsInstances, nil
}
