package msk

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/service/kafka"
)

//go:generate mockgen -source=msk.go -destination ../mocks/msk.go -package mocks

type MSK interface {
	ListClustersV2(ctx context.Context, input *kafka.ListClustersV2Input, optFns ...func(*kafka.Options)) (*kafka.ListClustersV2Output, error)
}
