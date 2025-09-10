package client

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/service/rds"
	rdsTypes "github.com/aws/aws-sdk-go-v2/service/rds/types"
)

type rdsClient struct {
	client *rds.Client
}

func newRDS(client *rds.Client) *rdsClient {
	return &rdsClient{client: client}
}

func (e *rdsClient) listRDSInstances(ctx context.Context) ([]rdsTypes.DBInstance, error) {
	o, err := e.client.DescribeDBInstances(ctx, nil)
	if err != nil {
		return nil, err
	}
	return o.DBInstances, nil
}
