package pricingstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
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

type PricingStore struct {
	logger *slog.Logger

	regions []ec2Types.Region

	// Maps a region to a map of units to prices
	pricePerUnitPerRegion map[string]*map[string]float64
	regionPricingMapLock  *sync.RWMutex

	fetchPrices PriceFetchFunc
}

type PricingStoreRefresher interface {
	PopulatePricingMap(ctx context.Context) error
	GetPricePerUnitPerRegion() map[string]*map[string]float64
}

func newPricingStore(logger *slog.Logger, regions []ec2Types.Region) *PricingStore {
	return &PricingStore{
		logger:                logger,
		pricePerUnitPerRegion: make(map[string]*map[string]float64),
		regions:               regions,
		regionPricingMapLock:  &sync.RWMutex{},
	}
}

// NewPricingStore creates a new PricingStore.
// It populates the store before it is used by the Collector.
func NewPricingStore(ctx context.Context, logger *slog.Logger, regions []ec2Types.Region, fetchPrices PriceFetchFunc) *PricingStore {
	p := newPricingStore(logger, regions)
	p.fetchPrices = fetchPrices

	return p.populate(ctx)
}

func (p *PricingStore) populate(ctx context.Context) *PricingStore {
	// Populate the store before it is used by the Collector.
	err := p.PopulatePricingMap(ctx)
	if err != nil {
		p.logger.Error("error populating pricing map", "error", err)
	}

	return p
}

// populatePricingMap fetches the pricing information for a product from the AWS Pricing API.
func (p *PricingStore) PopulatePricingMap(ctx context.Context) error {
	p.logger.LogAttrs(ctx, slog.LevelInfo, "Refreshing pricing map")
	var prices []string
	eg, ctx := errgroup.WithContext(ctx)
	eg.SetLimit(errGroupLimit)
	m := sync.Mutex{}
	for _, region := range p.regions {
		eg.Go(func() error {
			regionName := *region.RegionName
			p.logger.LogAttrs(ctx, slog.LevelDebug, "fetching pricing info", slog.String("region", regionName))

			priceList, err := p.listPrices(ctx, regionName)
			if err != nil {
				return err
			}

			m.Lock()
			prices = append(prices, priceList...)
			m.Unlock()
			return nil
		})
	}
	err := eg.Wait()
	if err != nil {
		return err
	}

	if err := p.populatePriceStore(prices); err != nil {
		return fmt.Errorf("%w: %w", errGeneratePricingMap, err)
	}

	return nil
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

// GetPricePerUnitPerRegion returns the pricePerUnitPerRegion map.
func (p *PricingStore) GetPricePerUnitPerRegion() map[string]*map[string]float64 {
	return p.pricePerUnitPerRegion
}

// populatePriceStore receives a json with a list of all the prices per product.
// It iterates over the products in the price list and parses the price for each product.
func (p *PricingStore) populatePriceStore(priceList []string) error {
	// Clear out price store only if we have new price data
	p.regionPricingMapLock.Lock()
	defer p.regionPricingMapLock.Unlock()

	for _, product := range priceList {
		var productInfo productInfo
		if err := json.Unmarshal([]byte(product), &productInfo); err != nil {
			p.logger.Error("error unmarshalling product output", "error", err)
			return err
		}

		region := productInfo.Product.Attributes.Region
		if p.pricePerUnitPerRegion[region] == nil {
			product := make(map[string]float64)
			p.pricePerUnitPerRegion[region] = &product
		}

		// Extract pricing information
		for _, term := range productInfo.Terms.OnDemand {
			for _, priceDimension := range term.PriceDimensions {
				price, err := strconv.ParseFloat(priceDimension.PricePerUnit[USD], 64)
				if err != nil {
					p.logger.Error(fmt.Sprintf("error parsing price: %s, skipping", err))
					continue
				}

				// The UsageType is the unit of the price.
				// Metrics should be created per UsageType unit of work.
				unit := productInfo.Product.Attributes.UsageType
				(*p.pricePerUnitPerRegion[region])[unit] = price
			}
		}
	}
	return nil
}
