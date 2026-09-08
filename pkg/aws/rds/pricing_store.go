package rds

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/grafana/cloudcost-exporter/pkg/aws/client"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/sync/errgroup"
)

// defaultPricingRefreshInterval is the pricing store's refresh cadence.
// Pricing is a stable, bulk per-region fetch from the Pricing API rather than
// a per-instance lookup, so it doesn't need to refresh as often as instance
// inventory. Mirrors GKE's PriceRefreshInterval.
const defaultPricingRefreshInterval = 24 * time.Hour

// pricingStore refreshes RDS pricing in the background and serves it to
// Collect from memory, off the scrape path. It is independent of
// instanceStore: pricing is listed in bulk per region from the Pricing API
// and keyed by product attributes, not driven by the listed instances.
// Collect joins the two stores by pricing key.
type pricingStore struct {
	logger            *slog.Logger
	pricingClient     client.Client // shared across regions; the Pricing API takes a region name, not a per-region client
	regions           []types.Region
	regionListTimeout time.Duration
	concurrency       int
	populateErrors    *prometheus.CounterVec

	prices *pricingMap

	populating atomic.Bool

	initialPopulationOnce sync.Once
	initialPopulation     chan struct{}
}

// newPricingStore returns a store that begins warming immediately in the
// background, independent of instanceStore.
func newPricingStore(ctx context.Context, logger *slog.Logger, config *Config, populateErrors *prometheus.CounterVec) *pricingStore {
	s := &pricingStore{
		logger:            logger.With("store", "pricing"),
		pricingClient:     config.Client,
		regions:           config.Regions,
		regionListTimeout: config.RegionListTimeout,
		concurrency:       populateConcurrency,
		populateErrors:    populateErrors,
		prices:            newPricingMap(),
		initialPopulation: make(chan struct{}),
	}
	go s.Populate(ctx)
	return s
}

// Done is closed once the first populate attempt finishes, successfully or not.
func (s *pricingStore) Done() <-chan struct{} {
	return s.initialPopulation
}

// Get returns the cached on-demand hourly price for a pricing key.
func (s *pricingStore) Get(key string) (float64, bool) {
	return s.prices.Get(key)
}

// Populate refreshes pricing for every region. It skips the tick if a
// previous populate is still running, fans out under a bounded errgroup, and
// logs, counts, and swallows per-region errors so one bad region never drops
// its siblings.
func (s *pricingStore) Populate(ctx context.Context) {
	// Drop overlapping populates: if a tick fires while the previous one is
	// still running, avoid doubling API load.
	if !s.populating.CompareAndSwap(false, true) {
		s.logger.LogAttrs(ctx, slog.LevelInfo, "populate already in progress, skipping tick")
		return
	}
	defer s.populating.Store(false)

	defer s.initialPopulationOnce.Do(func() {
		close(s.initialPopulation)
	})

	var eg errgroup.Group
	eg.SetLimit(s.concurrency)

	for _, region := range s.regions {
		if ctx.Err() != nil {
			break
		}
		regionName := *region.RegionName
		eg.Go(func() error {
			if ctx.Err() != nil {
				return nil
			}
			s.populateRegion(ctx, regionName)
			return nil // log and continue; don't drop sibling regions
		})
	}
	eg.Wait()
}

// populateRegion refreshes a single region's pricing, bounded by a
// per-region timeout so a hung listing call cannot wedge the background loop.
func (s *pricingStore) populateRegion(ctx context.Context, regionName string) {
	timeout := s.regionListTimeout
	if timeout <= 0 {
		timeout = defaultPopulateTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	priceList, err := s.pricingClient.ListRDSPrices(ctx, regionName)
	if err != nil {
		s.logger.LogAttrs(ctx, slog.LevelError, "error listing RDS prices",
			slog.String("region", regionName),
			slog.String("error", err.Error()))
		s.populateErrors.WithLabelValues("pricing", regionName, "list_prices").Inc()
		return
	}

	for _, product := range priceList {
		key, price, ok := parseRDSPriceProduct(ctx, product)
		if !ok {
			s.populateErrors.WithLabelValues("pricing", regionName, "parse_pricing").Inc()
			continue
		}
		s.prices.Set(key, price)
	}
}
