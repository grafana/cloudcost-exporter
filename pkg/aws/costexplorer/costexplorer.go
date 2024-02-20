package costexplorer

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
)

type CostExplorer interface {
	GetCostAndUsage(ctx context.Context, params *costexplorer.GetCostAndUsageInput, optFns ...func(*costexplorer.Options)) (*costexplorer.GetCostAndUsageOutput, error)
}
