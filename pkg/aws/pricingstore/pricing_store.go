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
	pricingTypes "github.com/aws/aws-sdk-go-v2/service/pricing/types"
	awsclient "github.com/grafana/cloudcost-exporter/pkg/aws/client"
	"golang.org/x/sync/errgroup"
)

const (
	// Generic pricing constant
	USD = "USD"
)

const (
	errGroupLimit = 5
)

var (
	errListPrices         = errors.New("error listing prices")
	errClientNotFound     = errors.New("no client found")
	errGeneratePricingMap = errors.New("error generating pricing map")

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

	// AWS region to client map
	awsRegionClientMap map[string]awsclient.Client
	regions            []ec2Types.Region

	// Maps a region to a map of units to prices
	pricePerUnitPerRegion map[string]*map[string]float64
	regionPricingMapLock  *sync.RWMutex

	filters []pricingTypes.Filter
}

type PricingStoreRefresher interface {
	PopulatePricingMap(ctx context.Context) error
	GetPricePerUnitPerRegion() map[string]*map[string]float64
}

// NewPricingStore creates a new PricingStore.
// It populates the store before it is used by the Collector.
func NewPricingStore(ctx context.Context, logger *slog.Logger, regions []ec2Types.Region, awsRegionClientMap map[string]awsclient.Client, filters []pricingTypes.Filter) *PricingStore {
	p := &PricingStore{
		logger:                logger,
		pricePerUnitPerRegion: make(map[string]*map[string]float64),
		regions:               regions,
		awsRegionClientMap:    awsRegionClientMap,
		filters:               filters,

		regionPricingMapLock: &sync.RWMutex{},
	}

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
			p.logger.LogAttrs(ctx, slog.LevelDebug, "fetching pricing info", slog.String("region", *region.RegionName))

			regionClient, ok := p.awsRegionClientMap[*region.RegionName]
			if !ok {
				return errClientNotFound
			}

			priceList, err := regionClient.ListEC2ServicePrices(ctx, *region.RegionName, p.filters)
			if err != nil {
				return fmt.Errorf("%w: %w", errListPrices, err)
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
