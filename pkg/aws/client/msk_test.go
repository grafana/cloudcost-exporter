package client

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	msk "github.com/aws/aws-sdk-go-v2/service/kafka"
	msktypes "github.com/aws/aws-sdk-go-v2/service/kafka/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubMSKClient struct {
	outputs []*msk.ListClustersV2Output
	err     error
	inputs  []*msk.ListClustersV2Input
}

func (s *stubMSKClient) ListClustersV2(ctx context.Context, input *msk.ListClustersV2Input, optFns ...func(*msk.Options)) (*msk.ListClustersV2Output, error) {
	copied := *input
	s.inputs = append(s.inputs, &copied)

	if s.err != nil {
		return nil, s.err
	}
	if len(s.outputs) == 0 {
		return &msk.ListClustersV2Output{}, nil
	}

	output := s.outputs[0]
	s.outputs = s.outputs[1:]
	return output, nil
}

func TestListMSKClusters(t *testing.T) {
	t.Run("paginates over ListClustersV2 responses", func(t *testing.T) {
		client := &stubMSKClient{
			outputs: []*msk.ListClustersV2Output{
				{
					ClusterInfoList: []msktypes.Cluster{
						{ClusterArn: aws.String("arn:aws:kafka:us-east-1:123456789012:cluster/test-1")},
					},
					NextToken: aws.String("next-page"),
				},
				{
					ClusterInfoList: []msktypes.Cluster{
						{ClusterArn: aws.String("arn:aws:kafka:us-east-1:123456789012:cluster/test-2")},
					},
				},
			},
		}

		service := newMSK(client)
		clusters, err := service.listMSKClusters(t.Context())
		require.NoError(t, err)
		require.Len(t, clusters, 2)
		require.Len(t, client.inputs, 2)

		assert.Equal(t, int32(100), aws.ToInt32(client.inputs[0].MaxResults))
		assert.Nil(t, client.inputs[0].NextToken)
		assert.Equal(t, "next-page", aws.ToString(client.inputs[1].NextToken))
	})

	t.Run("returns paginator errors", func(t *testing.T) {
		service := newMSK(&stubMSKClient{err: errors.New("boom")})
		_, err := service.listMSKClusters(t.Context())
		require.Error(t, err)
	})
}
