package elb

import (
	"log/slog"
	"os"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

var testLogger = slog.New(slog.NewTextHandler(os.Stdout, nil))

func TestNewELBPricingMap(t *testing.T) {
	pm := NewELBPricingMap(testLogger)
	assert.NotNil(t, pm)
	assert.NotNil(t, pm.pricing)
	assert.Empty(t, pm.pricing)
}

func TestSetAndGetRegionPricing(t *testing.T) {
	pm := NewELBPricingMap(testLogger)

	pricing := &RegionPricing{
		ALBHourlyRate: map[string]float64{"default": 0.0225},
		NLBHourlyRate: map[string]float64{"default": 0.0225},
	}

	// Test setting pricing
	pm.SetRegionPricing("us-east-1", pricing)

	// Test getting pricing
	retrieved, err := pm.GetRegionPricing("us-east-1")
	assert.NoError(t, err)
	assert.Equal(t, pricing, retrieved)

	// Test getting non-existent region
	notFound, err := pm.GetRegionPricing("non-existent")
	assert.Error(t, err)
	assert.Nil(t, notFound)
}

func TestConcurrentAccess(t *testing.T) {
	pm := NewELBPricingMap(testLogger)

	pricing1 := &RegionPricing{
		ALBHourlyRate: map[string]float64{"default": 0.0225},
		NLBHourlyRate: map[string]float64{"default": 0.0225},
	}

	pricing2 := &RegionPricing{
		ALBHourlyRate: map[string]float64{"default": 0.0250},
		NLBHourlyRate: map[string]float64{"default": 0.0250},
	}

	var wg sync.WaitGroup
	numGoroutines := 10

	// Test concurrent writes
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				pm.SetRegionPricing("us-east-1", pricing1)
			} else {
				pm.SetRegionPricing("us-west-2", pricing2)
			}
		}(i)
	}
	wg.Wait()

	// Test concurrent reads
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			p1, err := pm.GetRegionPricing("us-east-1")
			assert.NoError(t, err)
			p2, err := pm.GetRegionPricing("us-west-2")
			assert.NoError(t, err)
			assert.NotNil(t, p1)
			assert.NotNil(t, p2)
		}()
	}
	wg.Wait()

	// Verify final state
	finalPricing1, err := pm.GetRegionPricing("us-east-1")
	assert.NoError(t, err)
	finalPricing2, err := pm.GetRegionPricing("us-west-2")
	assert.NoError(t, err)

	assert.NotNil(t, finalPricing1)
	assert.NotNil(t, finalPricing2)
	assert.Equal(t, pricing1, finalPricing1)
	assert.Equal(t, pricing2, finalPricing2)
}
