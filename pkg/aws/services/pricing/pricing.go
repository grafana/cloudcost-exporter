package pricing

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/service/pricing"
)

type Pricing interface {
	GetProducts(ctx context.Context, params *pricing.GetProductsInput, optFns ...func(*pricing.Options)) (*pricing.GetProductsOutput, error)
}
