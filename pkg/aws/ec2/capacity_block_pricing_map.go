package ec2

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/grafana/cloudcost-exporter/pkg/aws/client"
)

var ErrCapacityBlockPriceNotFound = errors.New("no capacity block price found")

// CapacityBlockPricingMap holds the amortized hourly price of EC2 Capacity Block
// for ML instances, keyed by capacity reservation ID. The value is USD per
// instance-hour.
//
// Attribution is per-reservation: Cost Explorer keys the upfront fee only by region+type,
// but it dates the fee to the reservation's StartDate, so each active reservation is
// matched to the fee on its own StartDate day. This avoids smearing fees across renewed,
// expired, or concurrent same-type blocks — each instance is priced from its own
// reservation's fee via the instance's CapacityReservationId.
type CapacityBlockPricingMap struct {
	// Reservations maps a capacity reservation ID -> USD per instance-hour.
	Reservations map[string]float64
	m            sync.RWMutex
	logger       *slog.Logger
	cfgRegions   []ec2Types.Region
	regionMap    map[string]client.Client
}

func NewCapacityBlockPricingMap(l *slog.Logger, config *Config) *CapacityBlockPricingMap {
	return &CapacityBlockPricingMap{
		Reservations: make(map[string]float64),
		m:            sync.RWMutex{},
		logger:       l.With("subsystem", "capacityBlockPricing"),
		cfgRegions:   config.Regions,
		regionMap:    config.RegionMap,
	}
}

// reservationPricing holds the per-reservation inputs needed to price a block.
type reservationPricing struct {
	id            string
	region        string
	instanceType  string
	startDay      time.Time // StartDate truncated to the UTC day
	instanceHours float64   // TotalInstanceCount * (EndDate - StartDate)
}

// GenerateCapacityBlockPricingMap prices each active Capacity Block reservation
// as fee / (instance_count * block_hours), keyed by reservation ID.
//
// The fee comes from Cost Explorer, dated to the reservation's StartDate, so we
// query a one-day window on that day per reservation. Reservations that share a
// region+type+StartDate day are grouped and split proportionally by
// instance-hours, since Cost Explorer reports one summed fee for them (it exposes
// no reservation ID at that granularity).
func (m *CapacityBlockPricingMap) GenerateCapacityBlockPricingMap(ctx context.Context) error {
	m.logger.LogAttrs(ctx, slog.LevelInfo, "Refreshing capacity block pricing map")

	feeClient := m.billingClient()
	if feeClient == nil {
		return ErrClientNotFound
	}

	// Collect active capacity-block reservations across all configured regions.
	// DescribeCapacityReservations is regional, so each region client is queried.
	var reservations []reservationPricing
	for _, region := range m.cfgRegions {
		regionClient, ok := m.regionMap[*region.RegionName]
		if !ok {
			return ErrClientNotFound
		}
		active, err := regionClient.ListActiveCapacityReservations(ctx)
		if err != nil {
			return fmt.Errorf("listing capacity reservations in %s: %w", *region.RegionName, err)
		}
		for _, r := range active {
			if rp, ok := newReservationPricing(r); ok {
				reservations = append(reservations, rp)
			}
		}
	}

	// Group by region+type+StartDate day: Cost Explorer reports one summed fee
	// for reservations that share these, so they must be priced together.
	type groupKey struct {
		region       string
		instanceType string
		day          string
	}
	groups := make(map[groupKey][]reservationPricing)
	for _, r := range reservations {
		k := groupKey{r.region, r.instanceType, r.startDay.Format("2006-01-02")}
		groups[k] = append(groups[k], r)
	}

	newRates := make(map[string]float64)
	for key, group := range groups {
		// Fee is dated to StartDate; a one-day window on that UTC day isolates it.
		start := group[0].startDay
		end := start.AddDate(0, 0, 1)
		fees, err := feeClient.GetCapacityBlockCosts(ctx, start, end)
		if err != nil {
			m.logger.Error("failed to fetch capacity block fee",
				"region", key.region, "instance_type", key.instanceType, "start", key.day, "error", err)
			continue
		}
		fee, ok := fees.GetFee(key.region, key.instanceType)
		if !ok {
			m.logger.Warn("active capacity reservation has no fee in Cost Explorer, skipping",
				"region", key.region, "instance_type", key.instanceType, "start", key.day)
			continue
		}

		var totalInstanceHours float64
		for _, r := range group {
			totalInstanceHours += r.instanceHours
		}
		if totalInstanceHours <= 0 {
			continue
		}
		// Proportional split across the group reduces to a shared per-instance-hour
		// rate; totals are preserved regardless of the split.
		rate := fee / totalInstanceHours
		if len(group) > 1 {
			m.logger.Warn("multiple capacity reservations share region+type+start day; splitting fee by instance-hours",
				"region", key.region, "instance_type", key.instanceType, "start", key.day, "reservations", len(group))
		}
		for _, r := range group {
			newRates[r.id] = rate
			m.logger.Debug("computed capacity block rate",
				"reservation_id", r.id, "region", key.region, "instance_type", key.instanceType,
				"usd_per_instance_hour", rate, "fee_usd", fee, "group_instance_hours", totalInstanceHours)
		}
	}

	m.m.Lock()
	m.Reservations = newRates
	m.m.Unlock()
	return nil
}

// newReservationPricing extracts the pricing inputs from a reservation, or
// ok=false if any required field is missing or the duration is non-positive.
func newReservationPricing(r ec2Types.CapacityReservation) (reservationPricing, bool) {
	if r.CapacityReservationId == nil || r.InstanceType == nil || r.AvailabilityZone == nil ||
		r.StartDate == nil || r.EndDate == nil || r.TotalInstanceCount == nil {
		return reservationPricing{}, false
	}
	az := *r.AvailabilityZone
	if len(az) < 2 {
		return reservationPricing{}, false
	}
	hours := r.EndDate.Sub(*r.StartDate).Hours()
	if hours <= 0 {
		return reservationPricing{}, false
	}
	s := r.StartDate.UTC()
	return reservationPricing{
		id:           *r.CapacityReservationId,
		region:       az[:len(az)-1], // strip the AZ letter (e.g. us-east-2a -> us-east-2)
		instanceType: *r.InstanceType,
		startDay:     time.Date(s.Year(), s.Month(), s.Day(), 0, 0, 0, 0, time.UTC),
		// instance-hours = count * duration; the fee is amortized across all of them.
		instanceHours: hours * float64(*r.TotalInstanceCount),
	}, true
}

// billingClient returns the client used for the account-global Cost Explorer
// call. Every region client is configured with the same Cost Explorer service,
// so the first configured region's client is used deterministically.
func (m *CapacityBlockPricingMap) billingClient() client.Client {
	if len(m.cfgRegions) == 0 {
		return nil
	}
	return m.regionMap[*m.cfgRegions[0].RegionName]
}

// GetPriceForReservation returns the amortized USD per instance-hour for a
// capacity reservation, matched from a running instance's CapacityReservationId.
func (m *CapacityBlockPricingMap) GetPriceForReservation(reservationID string) (float64, error) {
	m.m.RLock()
	defer m.m.RUnlock()
	rate, ok := m.Reservations[reservationID]
	if !ok {
		return 0, ErrCapacityBlockPriceNotFound
	}
	return rate, nil
}
