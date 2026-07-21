package ec2

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/cloudcost-exporter/pkg/aws/client"
)

func newTestCapacityBlockPricingMap(region string, mc *mockClient) *CapacityBlockPricingMap {
	return NewCapacityBlockPricingMap(slog.Default(), &Config{
		Regions:   []ec2Types.Region{{RegionName: aws.String(region)}},
		RegionMap: map[string]client.Client{region: mc},
	})
}

// start returns a fixed StartDate and the matching EndDate for a duration.
func startEnd(durationHours float64) (time.Time, time.Time) {
	start := time.Date(2026, 7, 10, 17, 16, 0, 0, time.UTC)
	return start, start.Add(time.Duration(durationHours) * time.Hour)
}

func TestGenerateCapacityBlockPricingMap_pricesPerReservation(t *testing.T) {
	start, end := startEnd(336) // 14 days

	mc := &mockClient{
		capacityBlockCosts: &client.CapacityBlockCosts{
			Regions: map[string]map[string]float64{"us-east-2": {"p5.48xlarge": 14710.60}},
		},
		capacityReservations: []ec2Types.CapacityReservation{
			{
				CapacityReservationId: aws.String("cr-1"),
				InstanceType:          aws.String("p5.48xlarge"),
				AvailabilityZone:      aws.String("us-east-2a"),
				TotalInstanceCount:    aws.Int32(1),
				StartDate:             &start,
				EndDate:               &end,
			},
		},
	}

	m := newTestCapacityBlockPricingMap("us-east-2", mc)
	require.NoError(t, m.GenerateCapacityBlockPricingMap(context.Background()))

	got, err := m.GetPriceForReservation("cr-1")
	assert.NoError(t, err)
	assert.InDelta(t, 14710.60/336.0, got, 0.0001)
}

func TestGenerateCapacityBlockPricingMap_splitsSameDaySameTypeReservations(t *testing.T) {
	start, end := startEnd(100)

	// Two reservations sharing region+type+StartDate day: CE reports one summed
	// fee, split proportionally by instance-hours (2 * 100h = 200h).
	mc := &mockClient{
		capacityBlockCosts: &client.CapacityBlockCosts{
			Regions: map[string]map[string]float64{"us-east-2": {"p5.48xlarge": 2000.0}},
		},
		capacityReservations: []ec2Types.CapacityReservation{
			{CapacityReservationId: aws.String("cr-a"), InstanceType: aws.String("p5.48xlarge"), AvailabilityZone: aws.String("us-east-2a"), TotalInstanceCount: aws.Int32(1), StartDate: &start, EndDate: &end},
			{CapacityReservationId: aws.String("cr-b"), InstanceType: aws.String("p5.48xlarge"), AvailabilityZone: aws.String("us-east-2b"), TotalInstanceCount: aws.Int32(1), StartDate: &start, EndDate: &end},
		},
	}

	m := newTestCapacityBlockPricingMap("us-east-2", mc)
	require.NoError(t, m.GenerateCapacityBlockPricingMap(context.Background()))

	for _, id := range []string{"cr-a", "cr-b"} {
		got, err := m.GetPriceForReservation(id)
		assert.NoError(t, err, id)
		assert.InDelta(t, 2000.0/200.0, got, 0.0001, id)
	}
}

func TestGenerateCapacityBlockPricingMap_skipsReservationWithoutFee(t *testing.T) {
	start, end := startEnd(336)

	mc := &mockClient{
		capacityBlockCosts: &client.CapacityBlockCosts{Regions: map[string]map[string]float64{}}, // no fees
		capacityReservations: []ec2Types.CapacityReservation{
			{CapacityReservationId: aws.String("cr-1"), InstanceType: aws.String("p5.48xlarge"), AvailabilityZone: aws.String("us-east-2a"), TotalInstanceCount: aws.Int32(1), StartDate: &start, EndDate: &end},
		},
	}

	m := newTestCapacityBlockPricingMap("us-east-2", mc)
	require.NoError(t, m.GenerateCapacityBlockPricingMap(context.Background()))

	_, err := m.GetPriceForReservation("cr-1")
	assert.ErrorIs(t, err, ErrCapacityBlockPriceNotFound)
}
