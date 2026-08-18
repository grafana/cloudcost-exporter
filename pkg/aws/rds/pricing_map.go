package rds

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
)

type AWSPriceData struct {
	Product *AWSProduct `json:"product"`
	Terms   *AWSTerms   `json:"terms"`
}

type AWSProduct struct {
	Attributes *AWSProductAttributes `json:"attributes"`
}

// AWSProductAttributes holds the RDS Database Instance product attributes that
// make up a pricing key. These come straight from the AWS Pricing API, so the
// engine and edition are display names ("PostgreSQL", "Oracle"), not the
// instance Engine codes ("postgres", "oracle-ee").
type AWSProductAttributes struct {
	InstanceType     string `json:"instanceType"`
	RegionCode       string `json:"regionCode"`
	DatabaseEngine   string `json:"databaseEngine"`
	DatabaseEdition  string `json:"databaseEdition"`
	DeploymentOption string `json:"deploymentOption"`
	LicenseModel     string `json:"licenseModel"`
	LocationType     string `json:"locationType"`
}

type AWSTerms struct {
	OnDemand map[string]*AWSTerm `json:"OnDemand"`
}

type AWSTerm struct {
	PriceDimensions map[string]*AWSPriceDimension `json:"priceDimensions"`
}

type AWSPriceDimension struct {
	PricePerUnit map[string]string `json:"pricePerUnit"`
}

type pricingMap struct {
	pricingMap map[string]float64
	mu         sync.RWMutex
}

func newPricingMap() *pricingMap {
	return &pricingMap{
		pricingMap: make(map[string]float64),
		mu:         sync.RWMutex{},
	}
}

func (pm *pricingMap) Set(key string, value float64) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.pricingMap[key] = value
}

func (pm *pricingMap) Get(key string) (float64, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	v, ok := pm.pricingMap[key]
	return v, ok
}

// priceFromTerms extracts the single on-demand USD price from a product's
// pricing terms.
func priceFromTerms(ctx context.Context, terms *AWSTerms) (float64, error) {
	if terms == nil {
		slog.ErrorContext(ctx, "Terms is nil")
		return 0, fmt.Errorf("terms is nil")
	}
	if terms.OnDemand == nil {
		slog.ErrorContext(ctx, "OnDemand is nil")
		return 0, fmt.Errorf("OnDemand is nil")
	}

	var term *AWSTerm
	for _, t := range terms.OnDemand {
		if t == nil || t.PriceDimensions == nil {
			slog.ErrorContext(ctx, "PriceDimensions is nil")
			return 0, fmt.Errorf("PriceDimensions is nil")
		}
		term = t
		break
	}

	var dimension *AWSPriceDimension
	for _, d := range term.PriceDimensions {
		if d == nil || d.PricePerUnit == nil {
			slog.ErrorContext(ctx, "PricePerUnit is nil")
			return 0, fmt.Errorf("PricePerUnit is nil")
		}
		dimension = d
		break
	}

	priceStr, ok := dimension.PricePerUnit["USD"]
	if !ok || priceStr == "" {
		slog.ErrorContext(ctx, "No USD price found in PricePerUnit")
		return 0, fmt.Errorf("no USD price found in PricePerUnit")
	}

	price, err := strconv.ParseFloat(priceStr, 64)
	if err != nil {
		slog.ErrorContext(ctx, "error parsing price string to float", "priceStr", priceStr, "error", err)
		return 0, fmt.Errorf("error parsing price string '%s' to float: %w", priceStr, err)
	}

	return price, nil
}

// parseRDSPriceProduct turns a single Pricing API product into its pricing key
// and on-demand USD price. ok is false when the product lacks the attributes or
// on-demand price needed to key it, so the caller can count and skip it.
func parseRDSPriceProduct(ctx context.Context, priceList string) (key string, price float64, ok bool) {
	var priceData AWSPriceData
	if err := json.Unmarshal([]byte(priceList), &priceData); err != nil {
		slog.ErrorContext(ctx, "error unmarshaling RDS price JSON", "error", err)
		return "", 0, false
	}

	key, ok = priceKeyFromAttributes(priceData.Product)
	if !ok {
		return "", 0, false
	}

	price, err := priceFromTerms(ctx, priceData.Terms)
	if err != nil {
		return "", 0, false
	}
	return key, price, true
}

// priceKeyFromAttributes builds the pricing key from a product's attributes. It
// returns ok=false when an attribute the key depends on is missing.
func priceKeyFromAttributes(product *AWSProduct) (string, bool) {
	if product == nil || product.Attributes == nil {
		return "", false
	}
	a := product.Attributes
	if a.RegionCode == "" || a.InstanceType == "" || a.DatabaseEngine == "" || a.DeploymentOption == "" || a.LocationType == "" {
		return "", false
	}
	license := a.LicenseModel
	if a.DatabaseEdition == "" {
		// Open-source engines carry no edition; normalize their license to the
		// same token the instance side uses so the key matches without either
		// side depending on the raw licenseModel attribute string.
		license = openSourceLicense
	}
	return createPricingKey(a.RegionCode, a.InstanceType, a.DatabaseEngine, a.DatabaseEdition, a.DeploymentOption, license, a.LocationType), true
}
