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
	Terms *AWSTerms `json:"terms"`
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

func validateRDSPriceData(ctx context.Context, priceList string) (float64, error) {
	var priceData AWSPriceData
	if err := json.Unmarshal([]byte(priceList), &priceData); err != nil {
		slog.ErrorContext(ctx, "error unmarshaling price JSON", "error", err)
		return 0, err
	}

	if priceData.Terms == nil {
		slog.ErrorContext(ctx, "Terms is nil")
		return 0, fmt.Errorf("terms is nil")
	}
	if priceData.Terms.OnDemand == nil {
		slog.ErrorContext(ctx, "OnDemand is nil")
		return 0, fmt.Errorf("OnDemand is nil")
	}

	// example of onDemandPayload
	// {
	// 	"terms": {
	// 	  "OnDemand": {
	// 		"<termId>": {
	// 		  "priceDimensions": {
	// 			"<dimensionId>": {
	// 			  "pricePerUnit": {"USD": "0.0840000000"}
	// 			}
	// 		  }
	// 		}
	// 	  }
	// 	}
	//   }
	var term *AWSTerm
	// we iterate over the onDemand map to get the value
	// since the key is unknown (AWS id specific)
	for _, t := range priceData.Terms.OnDemand {
		if t == nil || t.PriceDimensions == nil {
			slog.ErrorContext(ctx, "PriceDimensions is nil")
			return 0, fmt.Errorf("PriceDimensions is nil")
		}
		term = t
		break
	}

	// same logic for the RDS price
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
