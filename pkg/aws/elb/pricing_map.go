package elb

import "sync"

type RegionPricing struct {
	ALBHourlyRate map[string]float64
	NLBHourlyRate map[string]float64
	CLBHourlyRate map[string]float64
}

type ELBPricingMap struct {
	mu      sync.RWMutex
	pricing map[string]*RegionPricing
}

func NewELBPricingMap() *ELBPricingMap {
	return &ELBPricingMap{
		pricing: make(map[string]*RegionPricing),
	}
}

func (pm *ELBPricingMap) SetRegionPricing(region string, pricing *RegionPricing) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.pricing[region] = pricing
}

func (pm *ELBPricingMap) GetRegionPricing(region string) *RegionPricing {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.pricing[region]
}