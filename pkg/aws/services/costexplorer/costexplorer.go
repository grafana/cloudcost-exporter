package costexplorer

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
)

//go:generate mockgen -source=costexplorer.go -destination ../mocks/costexplorer.go -package mocks

type CostExplorer interface {
	GetCostAndUsage(ctx context.Context, params *costexplorer.GetCostAndUsageInput, optFns ...func(*costexplorer.Options)) (*costexplorer.GetCostAndUsageOutput, error)
}
