package eks

import (
	"reflect"
	"testing"
)

func Test_weightedPriceForInstance(t *testing.T) {
	tests := map[string]struct {
		price   float64
		product productTerm
		want    ComputePrices
	}{
		"No cpu or ram": {
			price: 1.0,
			product: productTerm{
				Product: product{
					Attributes: Attributes{
						VCPU:   "",
						Memory: "",
					},
				},
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got, _ := weightedPriceForInstance(tt.args.price, tt.args.product); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("weightedPriceForInstance() = %v, want %v", got, tt.want)
			}
		})
	}
}
