package pricingstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"log/slog"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"golang.org/x/sync/errgroup"
)

// PriceFetchFunc fetches pricing product JSON strings for a given region.
// The caller provides all client/filter context via closure.
type PriceFetchFunc func(ctx context.Context, region string) ([]string, error)

const (
	// Generic pricing constant
	USD = "USD"
)

const (
	errGroupLimit = 5
)

var (
	errListPrices                = errors.New("error listing prices")
	errGeneratePricingMap        = errors.New("error generating pricing map")
	errPriceFetcherNotConfigured = errors.New("price fetcher not configured")

	PriceRefreshInterval = 24 * time.Hour
)

// productInfo represents the nested json response returned by the AWS pricing API EC2
type productInfo struct {
	Product struct {
		Attributes struct {
			UsageType string `json:"usagetype"`
			Region    string `json:"regionCode"`
		}
	}
	Terms struct {
		OnDemand map[string]struct {
			PriceDimensions map[string]struct {
				PricePerUnit map[string]string `json:"pricePerUnit"`
			}
		}
	}
}

type priceSnapshot struct {
	byRegion map[string]map[string]float64
}

// Snapshot is an immutable view of the pricing data published by the store.
type Snapshot struct {
	ptr *priceSnapshot
}

// Regions yields each region and its pricing view from the snapshot.
func (s Snapshot) Regions() iter.Seq2[string, RegionSnapshot] {
	return func(yield func(string, RegionSnapshot) bool) {
		if s.ptr == nil {
			return
		}

		for region, prices := range s.ptr.byRegion {
			if !yield(region, RegionSnapshot{prices: prices}) {
				return
			}
		}
	}
}

// Region returns the pricing view for one region.
func (s Snapshot) Region(region string) (RegionSnapshot, bool) {
	if s.ptr == nil {
		return RegionSnapshot{}, false
	}

	prices, ok := s.ptr.byRegion[region]
	if !ok {
		return RegionSnapshot{}, false
	}

	return RegionSnapshot{prices: prices}, true
}

// RegionSnapshot is an immutable view of one region's pricing data.
type RegionSnapshot struct {
	prices map[string]float64
}

// Get returns one exact usage type price.
func (r RegionSnapshot) Get(usageType string) (float64, bool) {
	price, ok := r.prices[usageType]
	return price, ok
}

// Entries yields each usage type and price from the region snapshot.
func (r RegionSnapshot) Entries() iter.Seq2[string, float64] {
	return func(yield func(string, float64) bool) {
		for usageType, price := range r.prices {
			if !yield(usageType, price) {
				return
			}
		}
	}
}

type PricingStore struct {
	logger *slog.Logger

	regions []ec2Types.Region

	// Snapshots are built off to the side and swapped atomically so readers
	// always see a consistent view without taking locks.
	current atomic.Pointer[priceSnapshot]

	fetchPrices PriceFetchFunc
}

type PricingStoreRefresher interface {
	PopulatePricingMap(ctx context.Context) error
	Snapshot() Snapshot
}

// NewPricingStore creates a new PricingStore and populates it synchronously.
// It always returns a non-nil store. If the initial populate fails, the store
// is empty and the error is returned so callers can decide whether to proceed or abort.
func NewPricingStore(ctx context.Context, logger *slog.Logger, regions []ec2Types.Region, fetchPrices PriceFetchFunc) (*PricingStore, error) {
	store := &PricingStore{
		logger:      logger,
		regions:     regions,
		fetchPrices: fetchPrices,
	}
	store.current.Store(&priceSnapshot{byRegion: make(map[string]map[string]float64)})

	if err := store.PopulatePricingMap(ctx); err != nil {
		return store, err
	}

	return store, nil
}

// PopulatePricingMap fetches pricing information and publishes a new snapshot.
func (p *PricingStore) PopulatePricingMap(ctx context.Context) error {
	p.logger.LogAttrs(ctx, slog.LevelInfo, "Refreshing pricing map")

	priceList, err := p.fetchAllPrices(ctx)
	if err != nil {
		return err
	}

	next, err := p.buildSnapshot(priceList)
	if err != nil {
		return fmt.Errorf("%w: %w", errGeneratePricingMap, err)
	}

	p.current.Store(next)
	return nil
}

func (p *PricingStore) fetchAllPrices(ctx context.Context) ([]string, error) {
	var prices []string

	eg, ctx := errgroup.WithContext(ctx)
	eg.SetLimit(errGroupLimit)

	var mu sync.Mutex
	for _, region := range p.regions {
		regionName := *region.RegionName
		eg.Go(func() error {
			p.logger.LogAttrs(ctx, slog.LevelDebug, "fetching pricing info", slog.String("region", regionName))

			priceList, err := p.listPrices(ctx, regionName)
			if err != nil {
				return err
			}

			mu.Lock()
			prices = append(prices, priceList...)
			mu.Unlock()

			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return nil, err
	}

	return prices, nil
}

func (p *PricingStore) listPrices(ctx context.Context, region string) ([]string, error) {
	if p.fetchPrices == nil {
		return nil, errPriceFetcherNotConfigured
	}

	priceList, err := p.fetchPrices(ctx, region)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errListPrices, err)
	}

	return priceList, nil
}

func (p *PricingStore) Snapshot() Snapshot {
	return Snapshot{ptr: p.current.Load()}
}

func (p *PricingStore) buildSnapshot(priceList []string) (*priceSnapshot, error) {
	snapshot := &priceSnapshot{byRegion: make(map[string]map[string]float64)}

	for _, product := range priceList {
		var productInfo productInfo
		if err := json.Unmarshal([]byte(product), &productInfo); err != nil {
			p.logger.Error("error unmarshalling product output", "error", err)
			return nil, err
		}

		region := productInfo.Product.Attributes.Region
		regionPrices := snapshot.byRegion[region]
		if regionPrices == nil {
			regionPrices = make(map[string]float64)
			snapshot.byRegion[region] = regionPrices
		}

		// The UsageType is the unit of the price.
		// Metrics should be created per UsageType unit of work.
		unit := productInfo.Product.Attributes.UsageType
		for _, term := range productInfo.Terms.OnDemand {
			for _, priceDimension := range term.PriceDimensions {
				price, err := strconv.ParseFloat(priceDimension.PricePerUnit[USD], 64)
				if err != nil {
					p.logger.Error(fmt.Sprintf("error parsing price: %s, skipping", err))
					continue
				}
				regionPrices[unit] = price
			}
		}
	}

	return snapshot, nil
}
