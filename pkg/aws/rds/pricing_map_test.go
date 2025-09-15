package rds

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateRDSPriceData(t *testing.T) {
	ctx := context.Background()

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
