package pricing

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/grafana/cloudcost-exporter/pkg/google/services/mocks"
)

func TestGetOperationsDiscounts(t *testing.T) {
	mockService := mocks.NewService(t)
	mockService.On("GetOperationsDiscounts").Return(map[string]map[string]map[string]float64{
		"region": {
			"archive": {
				"class-a": 0.190,
			},
		},
	}, nil)
	discounts := mockService.GetOperationsDiscounts()
	assert.Equal(t, discounts["region"]["archive"]["class-a"], 0.190)
}
