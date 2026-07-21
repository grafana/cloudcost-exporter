package client

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/aws/aws-sdk-go-v2/config"
	awsec2 "github.com/aws/aws-sdk-go-v2/service/ec2"
)

// TestIntegration_listActiveCapacityReservations hits the real EC2 API and prints
// the active Capacity Block reservations it finds. It is skipped unless
// EC2_INTEGRATION=1, so it never runs in CI.
//
// Unlike Cost Explorer, DescribeCapacityReservations is per-account and regional,
// so use creds for the account that owns the reservation and set the region to
// where the block lives:
//
//	AWS_PROFILE=<profile> AWS_REGION=us-east-2 EC2_INTEGRATION=1 \
//	  go test ./pkg/aws/client/ -run TestIntegration_listActiveCapacityReservations -v
func TestIntegration_listActiveCapacityReservations(t *testing.T) {
	if os.Getenv("EC2_INTEGRATION") != "1" {
		t.Skip("set EC2_INTEGRATION=1 to run against the real EC2 API")
	}

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})))

	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		t.Fatalf("load AWS config: %v", err)
	}

	c := newCompute(awsec2.NewFromConfig(cfg))
	reservations, err := c.listActiveCapacityReservations(ctx)
	if err != nil {
		t.Fatalf("listActiveCapacityReservations: %v", err)
	}

	if len(reservations) == 0 {
		t.Logf("no active capacity-block reservations found (check region/account)")
	}
	for _, r := range reservations {
		var count int32
		if r.TotalInstanceCount != nil {
			count = *r.TotalInstanceCount
		}
		t.Logf("id=%s type=%s az=%s count=%d start=%v end=%v",
			deref(r.CapacityReservationId), deref(r.InstanceType), deref(r.AvailabilityZone),
			count, r.StartDate, r.EndDate)
	}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
