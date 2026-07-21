package client

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
)

// TestIntegration_getCapacityBlockCosts hits the real Cost Explorer API and prints
// the net Capacity Block fees it finds. It is skipped unless CE_INTEGRATION=1, so
// it never runs in CI. Run with AWS creds for an account (or the payer account,
// which sees all linked accounts) that has Capacity Block fees. Cost Explorer is
// only reachable via us-east-1.
//
//	AWS_PROFILE=management CE_INTEGRATION=1 \
//	  go test ./pkg/aws/client/ -run TestIntegration_getCapacityBlockCosts -v
func TestIntegration_getCapacityBlockCosts(t *testing.T) {
	if os.Getenv("CE_INTEGRATION") != "1" {
		t.Skip("set CE_INTEGRATION=1 to run against the real Cost Explorer API")
	}

	// Debug level so the per-row parse/process traces in parseCapacityBlockCosts
	// are visible.
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})))

	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion("us-east-1"))
	if err != nil {
		t.Fatalf("load AWS config: %v", err)
	}

	b := newBilling(costexplorer.NewFromConfig(cfg), NewMetrics())

	end := time.Now().UTC()
	start := end.AddDate(0, 0, -30)
	costs, err := b.getCapacityBlockCosts(ctx, start, end)
	if err != nil {
		t.Fatalf("getCapacityBlockCosts: %v", err)
	}

	if len(costs.Regions) == 0 {
		t.Logf("no CapacityBlockFee charges found between %s and %s",
			start.Format("2006-01-02"), end.Format("2006-01-02"))
	}
	for region, byType := range costs.Regions {
		for instanceType, fee := range byType {
			t.Logf("region=%s instance_type=%s net_fee_usd=%.2f", region, instanceType, fee)
		}
	}
}
