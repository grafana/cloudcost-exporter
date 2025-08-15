package natgateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"

	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	awsclient "github.com/grafana/cloudcost-exporter/pkg/aws/client"
	"golang.org/x/sync/errgroup"
)

const (
	// NAT Gateway usage types
	NATGatewayHours = "NatGateway-Hours"
	NATGatewayBytes = "NatGateway-Bytes"

	// Generic pricing constant
	USD = "USD"
)

var (
	errListPrices         = errors.New("error listing prices")
	errClientNotFound     = errors.New("no client found")
	errGeneratePricingMap = errors.New("error generating pricing map")
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
	// Maps a region to a map of units to prices
	pricePerUnitPerRegion map[string]*map[string]float64
	regions               []ec2Types.Region
	awsRegionClientMap    map[string]awsclient.Client

	m      sync.RWMutex
	logger *slog.Logger
}

func NewPricingStore(logger *slog.Logger, regions []ec2Types.Region, awsRegionClientMap map[string]awsclient.Client) *PricingStore {
	return &PricingStore{
		logger:                logger,
		pricePerUnitPerRegion: make(map[string]*map[string]float64),
		regions:               regions,
		awsRegionClientMap:    awsRegionClientMap,
	}
}

// GeneratePricingMap receives a json with a list of all the prices per product.
// It iterates over the products in the price list and parses the price for each product.
func (p *PricingStore) populatePriceStore(priceList []string) error {
	p.m.Lock()
	defer p.m.Unlock()

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

				// TODO: Make this generic
				unit := productInfo.Product.Attributes.UsageType
				(*p.pricePerUnitPerRegion[region])[unit] = price
			}
		}
	}
	return nil
}

func (p *PricingStore) populatePricingMap(ctx context.Context) error {
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

			// TODO: Create a generic ListPrices endpoint
			// that takes a awsPricing.GetProductsInput{}
			// with a helper func to build the input
			priceList, err := regionClient.ListNATGatewayPrices(ctx, *region.RegionName)
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
