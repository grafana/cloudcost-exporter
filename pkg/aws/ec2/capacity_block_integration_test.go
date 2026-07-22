package ec2

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	awsec2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/grafana/cloudcost-exporter/pkg/aws/client"
)

// TestIntegration_generateCapacityBlockPricingMap builds the pricing map against
// the real AWS APIs (Cost Explorer for the fee, DescribeCapacityReservations for
// duration/count) and prints the amortized per-instance-hour prices it collects.
// Skipped unless CB_INTEGRATION=1.
//
// DescribeCapacityReservations is per-account and regional, so use creds for the
// account that owns the reservation and set the region where the block lives.
// Cost Explorer is account-global and only reachable via us-east-1, which the
// test configures separately (mirroring the production client wiring):
//
//	AWS_PROFILE=<profile> AWS_REGION=us-east-2 CB_INTEGRATION=1 \
//	  go test ./pkg/aws/ec2/ -run TestIntegration_generateCapacityBlockPricingMap -v
func TestIntegration_generateCapacityBlockPricingMap(t *testing.T) {
	if os.Getenv("CB_INTEGRATION") != "1" {
		t.Skip("set CB_INTEGRATION=1 to run against the real AWS APIs")
	}

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})))

	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		t.Fatalf("load AWS config: %v", err)
	}
	region := cfg.Region
	if region == "" {
		t.Fatal("no region configured; set AWS_REGION to where the Capacity Block lives")
	}
	// Cost Explorer is only reachable via us-east-1, regardless of the EC2 region.
	globalCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion("us-east-1"))
	if err != nil {
		t.Fatalf("load global AWS config: %v", err)
	}

	awsClient := client.NewAWSClient(client.Config{
		EC2Service:     awsec2.NewFromConfig(cfg),
		BillingService: costexplorer.NewFromConfig(globalCfg),
	})

	m := NewCapacityBlockPricingMap(slog.Default(), &Config{
		Regions:   []ec2Types.Region{{RegionName: aws.String(region)}},
		RegionMap: map[string]client.Client{region: awsClient},
	})

	if err := m.GenerateCapacityBlockPricingMap(ctx); err != nil {
		t.Fatalf("GenerateCapacityBlockPricingMap: %v", err)
	}

	if len(m.Reservations) == 0 {
		t.Logf("no capacity block prices found (check region/account; the reservation may have expired)")
	}
	for id, rate := range m.Reservations {
		t.Logf("reservation_id=%s usd_per_instance_hour=%.4f", id, rate)
	}
}
