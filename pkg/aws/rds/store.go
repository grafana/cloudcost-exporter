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
// Instances and prices are refreshed together because RDS prices are keyed by
// attributes only known from the live instances, unlike EC2's bulk pricing.
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

// populateRegion lists a single region's instances and warms their prices.
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

	s.warmPricing(ctx, regionName, instances)
}

// warmPricing fetches a price for each distinct pricing key among the region's
// instances and writes it to the shared pricing map, so Collect never issues a
// pricing call. Keys already fetched this pass are skipped to collapse the
// serialized fan-in that caused the cold-start p99.
func (s *instanceStore) warmPricing(ctx context.Context, regionName string, instances []rdsTypes.DBInstance) {
	seen := make(map[string]struct{})
	for _, instance := range instances {
		key, region, ok := pricingKeyFor(instance)
		if !ok {
			// AWS can return partial instances mid create or delete; skip them
			// rather than panic in this background goroutine.
			continue
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}

		depOption := multiOrSingleAZ(*instance.MultiAZ)
		locationType := isOutpostsInstance(instance)
		v, err := s.pricingClient.GetRDSUnitData(ctx, *instance.DBInstanceClass, region, depOption, *instance.Engine, locationType)
		if err != nil {
			s.logger.LogAttrs(ctx, slog.LevelError, "error fetching RDS price",
				slog.String("region", region),
				slog.String("instanceType", *instance.DBInstanceClass),
				slog.String("engine", *instance.Engine),
				slog.String("error", err.Error()))
			s.populateErrors.WithLabelValues("instances", regionName, "get_pricing").Inc()
			continue
		}
		if v == "" {
			s.logger.LogAttrs(ctx, slog.LevelWarn, "no pricing data found for RDS instance, skipping",
				slog.String("region", region),
				slog.String("instanceType", *instance.DBInstanceClass),
				slog.String("engine", *instance.Engine))
			continue
		}
		price, err := validateRDSPriceData(ctx, v)
		if err != nil {
			s.logger.LogAttrs(ctx, slog.LevelError, "error validating RDS price data",
				slog.String("region", region),
				slog.String("instanceType", *instance.DBInstanceClass),
				slog.String("error", err.Error()))
			s.populateErrors.WithLabelValues("instances", regionName, "validate_pricing").Inc()
			continue
		}
		s.pricingMap.Set(key, price)
	}
}
