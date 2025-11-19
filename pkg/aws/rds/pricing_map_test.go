package rds

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateRDSPriceData(t *testing.T) {
	ctx := t.Context()

	tests := []struct {
		name      string
		priceList string
		want      float64
		wantErr   bool
	}{
		{
			name: "valid price data",
			priceList: `{
				"terms": {
					"OnDemand": {
						"some-term-id": {
							"priceDimensions": {
								"some-dimension-id": {
									"pricePerUnit": {"USD": "0.0840000000"}
								}
							}
						}
					}
				}
			}`,
			want:    0.084,
			wantErr: false,
		},
		{
			name:      "invalid JSON",
			priceList: `{invalid json}`,
			want:      0.0,
			wantErr:   true,
		},
		{
			name:      "missing terms",
			priceList: `{}`,
			want:      0.0,
			wantErr:   true,
		},
		{
			name:      "missing OnDemand",
			priceList: `{"terms":{}}`,
			want:      0.0,
			wantErr:   true,
		},
		{
			name: "missing priceDimensions",
			priceList: `{
				"terms": {
					"OnDemand": {
						"term1": {}
					}
				}
			}`,
			want:    0.0,
			wantErr: true,
		},
		{
			name: "more than one term",
			priceList: `{
				"terms": {
					"OnDemand": {
						"term1": {},
						"term2": {}
					}
				}
			}`,
			want:    0.0,
			wantErr: true,
		},
		{
			name: "more than one price dimension",
			priceList: `{
				"terms": {
					"OnDemand": {
						"term1": {
							"priceDimensions": {
								"dim1": {},
								"dim2": {}
							}
						}
					}
				}
			}`,
			want:    0.0,
			wantErr: true,
		},
		{
			name: "missing pricePerUnit",
			priceList: `{
				"terms": {
					"OnDemand": {
						"term1": {
							"priceDimensions": {
								"dim1": {}
							}
						}
					}
				}
			}`,
			want:    0.0,
			wantErr: true,
		},
		{
			name: "missing USD in pricePerUnit",
			priceList: `{
				"terms": {
					"OnDemand": {
						"term1": {
							"priceDimensions": {
								"dim1": {
									"pricePerUnit": {}
								}
							}
						}
					}
				}
			}`,
			want:    0.0,
			wantErr: true,
		},
		{
			name: "invalid price value",
			priceList: `{
				"terms": {
					"OnDemand": {
						"term1": {
							"priceDimensions": {
								"dim1": {
									"pricePerUnit": {"USD": "not-a-number"}
								}
							}
						}
					}
				}
			}`,
			want:    0.0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			price, err := validateRDSPriceData(ctx, tt.priceList)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tt.want, price)
		})
	}
}

func TestPricingMap_SetAndGet(t *testing.T) {
	tests := []struct {
		name       string
		key        string
		pricingMap *pricingMap
		wantValue  float64
		wantOk     bool
	}{
		{
			name:       "set and get existing key",
			pricingMap: &pricingMap{pricingMap: map[string]float64{"test": 1.0}},
			key:        "test",
			wantValue:  1.0,
			wantOk:     true,
		},
		{
			name:       "get non-existing key",
			pricingMap: &pricingMap{pricingMap: map[string]float64{}},
			key:        "test",
			wantValue:  0.0,
			wantOk:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			price, ok := tt.pricingMap.Get(tt.key)
			assert.Equal(t, tt.wantValue, price)
			assert.Equal(t, tt.wantOk, ok)
		})
	}
}

func TestPricingMap_Concurrent(t *testing.T) {
	t.Run("concurrent read/write", func(t *testing.T) {
		pm := newPricingMap()

		for i := range 100 {
			key := fmt.Sprintf("key_%d", i)
			pm.Set(key, float64(i))
		}

		var wg sync.WaitGroup

		for i := range 50 {
			wg.Add(1)
			go func(key int) {
				defer wg.Done()
				for j := range 100 {
					key := fmt.Sprintf("key_%d", j)
					value, ok := pm.Get(key)
					if ok {
						assert.GreaterOrEqual(t, value, 0.0, "Reader %d: Value should be >= 0", key)
						assert.LessOrEqual(t, value, 100.0, "Reader %d: Value should be <= 100", key)
					}
				}
			}(i)
		}

		for i := range 50 {
			wg.Add(1)
			go func(key int) {
				defer wg.Done()
				for k := range 100 {
					key := fmt.Sprintf("key_%d", key)
					value := float64(k)
					pm.Set(key, value)
				}
			}(i)
		}

		wg.Wait()

		for i := range 100 {
			key := fmt.Sprintf("key_%d", i)
			_, ok := pm.Get(key)
			assert.True(t, ok, "Key %s should exist", key)
		}
	})
}
