package costexplorer

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
)

var (
	_ CostExplorer = Client{}
)

type CostExplorer interface {
	GetCostAndUsage(ctx context.Context, params *costexplorer.GetCostAndUsageInput, optFns ...func(*costexplorer.Options)) (*costexplorer.GetCostAndUsageOutput, error)
}

type Client struct {
	c CostExplorer
}

func (c Client) GetCostAndUsage(ctx context.Context, params *costexplorer.GetCostAndUsageInput, optFns ...func(*costexplorer.Options)) (*costexplorer.GetCostAndUsageOutput, error) {
	return c.GetCostAndUsage(ctx, params, optFns...)
}
