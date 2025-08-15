package natgateway

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
)

const (
	// NAT Gateway usage types
	NATGatewayHours = "NatGateway-Hours"
	NATGatewayBytes = "NatGateway-Bytes"

	// Generic pricing constant
	USD = "USD"
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

	m      sync.RWMutex
	logger *slog.Logger
}

func NewPricingStore(logger *slog.Logger) *PricingStore {
	return &PricingStore{
		logger:                logger,
		pricePerUnitPerRegion: make(map[string]*map[string]float64),
	}
}

// GeneratePricingMap receives a json with a list of all the prices per product.
// It iterates over the products in the price list and parses the price for each product.
func (p *PricingStore) PopulatePriceStore(priceList []string) error {
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
