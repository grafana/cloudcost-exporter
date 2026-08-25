package rds

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	rdsTypes "github.com/aws/aws-sdk-go-v2/service/rds/types"
	cloudcost_exporter "github.com/grafana/cloudcost-exporter"
	"github.com/grafana/cloudcost-exporter/pkg/aws/client"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/sync/errgroup"
)

// populateConcurrency caps the number of regions refreshed in parallel per
// populate. RDS is regional (one DescribeDBInstances call per region), so this
// bounds in-flight listing plus the pricing lookups each region triggers.
const populateConcurrency = 10

// defaultPopulateTimeout caps a single region's background refresh (listing
// plus pricing) when no RegionListTimeout is configured. On the scrape path the
// collector interval bounded these calls; the background populate has no such
// outer deadline, so without a ceiling a hung AWS call would keep Populate from
// returning, leave the readiness channel open, and skip every future tick. This
// ceiling guarantees the loop always makes progress.
const defaultPopulateTimeout = 2 * time.Minute

const defaultRefreshInterval = time.Hour

func newPopulateErrorsCounter() *prometheus.CounterVec {
	return prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: prometheus.BuildFQName(cloudcost_exporter.ExporterName, subsystem, "populate_errors_total"),
			Help: "Total errors during background store population, by store, region, and operation.",
		},
		[]string{"store", "region", "operation"},
	)
}

// startRefreshTicker drives a refresh loop until ctx is cancelled. It mirrors
// the GKE background-store helper of the same name.
func startRefreshTicker(ctx context.Context, interval time.Duration, run func()) {
	if interval <= 0 {
		interval = defaultRefreshInterval
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				run()
			}
		}
	}()
}

// instanceStore refreshes RDS instance inventory and pricing in the background
// and serves both to Collect from memory. Listing (the scrape average) and
// pricing lookups (the cold-start p99 tail) both move off the scrape path.
// Inventory and pricing are refreshed independently: prices are listed in bulk
// from the Pricing API and keyed by product attributes, then joined to instances
// by pricing key at Collect.
type instanceStore struct {
	logger            *slog.Logger
	regions           []types.Region
	regionMap         map[string]client.Client
	pricingClient     client.Client
	pricingMap        *pricingMap
	regionListTimeout time.Duration
	concurrency       int
	populateErrors    *prometheus.CounterVec

	mu        sync.RWMutex
	instances map[string][]rdsTypes.DBInstance // region -> instances

	populating atomic.Bool

	initialPopulationOnce sync.Once
	initialPopulation     chan struct{}
}

// newInstanceStore returns a store that begins warming immediately in the
// background. Warming at startup rather than on the first scrape closes the
// cold-start pricing gap that drove the RDS p99.
func newInstanceStore(ctx context.Context, logger *slog.Logger, config *Config, pm *pricingMap, populateErrors *prometheus.CounterVec) *instanceStore {
	s := &instanceStore{
		logger:            logger.With("store", "instances"),
		regions:           config.Regions,
		regionMap:         config.RegionMap,
		pricingClient:     config.Client,
		pricingMap:        pm,
		regionListTimeout: config.RegionListTimeout,
		concurrency:       populateConcurrency,
		populateErrors:    populateErrors,
		instances:         make(map[string][]rdsTypes.DBInstance),
		initialPopulation: make(chan struct{}),
	}
	go s.Populate(ctx)
	return s
}

// Done is closed once the first populate attempt finishes, successfully or not.
func (s *instanceStore) Done() <-chan struct{} {
	return s.initialPopulation
}

// Get returns the cached instances for a region.
func (s *instanceStore) Get(region string) []rdsTypes.DBInstance {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.instances[region]
}

// Populate refreshes instance inventory and pricing for every region. It skips
// the tick if a previous populate is still running, fans out under a bounded
// errgroup, and logs, counts, and swallows per-region errors so one bad region
// never drops its siblings.
func (s *instanceStore) Populate(ctx context.Context) {
	// Drop overlapping populates: if a tick fires while the previous one is
	// still running (slow AWS / many regions), avoid doubling API load and the
	// last-writer race on s.instances.
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
		regionClient, ok := s.regionMap[regionName]
		if !ok {
			s.logger.LogAttrs(ctx, slog.LevelError, "no client found for region", slog.String("region", regionName))
			s.populateErrors.WithLabelValues("instances", regionName, "lookup_client").Inc()
			continue
		}
		eg.Go(func() error {
			if ctx.Err() != nil {
				return nil
			}
			s.populateRegion(ctx, regionName, regionClient)
			return nil // log and continue; don't drop sibling regions
		})
	}
	eg.Wait()
}

// populateRegion refreshes a single region's instance inventory and pricing.
// The two are independent: pricing is listed in bulk from the Pricing API and
// keyed by product attributes, not driven by the listed instances. Collect
// joins them by pricing key.
func (s *instanceStore) populateRegion(ctx context.Context, regionName string, regionClient client.Client) {
	// Bound every AWS call for this region. A configured RegionListTimeout wins
	// so operators can fail slow regions fast; otherwise a safety ceiling keeps
	// a hung listing or pricing call from wedging the background loop.
	timeout := s.regionListTimeout
	if timeout <= 0 {
		timeout = defaultPopulateTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	s.populateInstances(ctx, regionName, regionClient)
	s.populatePricing(ctx, regionName)
}

// populateInstances lists a region's instances and caches them.
func (s *instanceStore) populateInstances(ctx context.Context, regionName string, regionClient client.Client) {
	instances, err := regionClient.ListRDSInstances(ctx)
	if err != nil {
		s.logger.LogAttrs(ctx, slog.LevelError, "error listing RDS instances",
			slog.String("region", regionName),
			slog.String("error", err.Error()))
		s.populateErrors.WithLabelValues("instances", regionName, "list_instances").Inc()
		return
	}

	s.mu.Lock()
	s.instances[regionName] = instances
	s.mu.Unlock()
}

// populatePricing lists every RDS Database Instance price in a region and writes
// each keyed price to the shared pricing map, so Collect never issues a pricing
// call. It goes through the dedicated pricing client and lists prices in bulk,
// keeping pricing independent of the instance inventory.
func (s *instanceStore) populatePricing(ctx context.Context, regionName string) {
	priceList, err := s.pricingClient.ListRDSPrices(ctx, regionName)
	if err != nil {
		s.logger.LogAttrs(ctx, slog.LevelError, "error listing RDS prices",
			slog.String("region", regionName),
			slog.String("error", err.Error()))
		s.populateErrors.WithLabelValues("instances", regionName, "list_prices").Inc()
		return
	}

	for _, product := range priceList {
		key, price, ok := parseRDSPriceProduct(ctx, product)
		if !ok {
			s.populateErrors.WithLabelValues("instances", regionName, "parse_pricing").Inc()
			continue
		}
		s.pricingMap.Set(key, price)
	}
}
