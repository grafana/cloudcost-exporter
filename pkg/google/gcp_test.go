package google

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/grafana/cloudcost-exporter/pkg/provider"
	mock_provider "github.com/grafana/cloudcost-exporter/pkg/provider/mocks"
)

func Test_RegisterCollectors(t *testing.T) {
	tests := map[string]struct {
		numCollectors int
		register      func(r provider.Registry) error
		expectedError error
	}{
		"no error if no collectors": {},
		"bubble-up single collector error": {
			numCollectors: 1,
			register: func(r provider.Registry) error {
				return fmt.Errorf("test register error")
			},
			expectedError: fmt.Errorf("test register error"),
		},
		"two collectors with no errors": {
			numCollectors: 2,
			register:      func(r provider.Registry) error { return nil },
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			r := mock_provider.NewMockRegistry(ctrl)
			c := mock_provider.NewMockCollector(ctrl)
			if tt.register != nil {
				c.EXPECT().Register(r).DoAndReturn(tt.register).Times(tt.numCollectors)
			}
			gcp := &GCP{
				config:     &Config{},
				collectors: []provider.Collector{},
			}
			for i := 0; i < tt.numCollectors; i++ {
				gcp.collectors = append(gcp.collectors, c)
			}
			err := gcp.RegisterCollectors(r)
			if tt.expectedError != nil {
				require.EqualError(t, err, tt.expectedError.Error())
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestGCP_CollectMetrics(t *testing.T) {
	tests := map[string]struct {
		numCollectors int
		collect       func() error
		expectedError error
	}{
		"no error if no collectors": {},
		"bubble-up single collector error": {
			numCollectors: 1,
			collect: func() error {
				return fmt.Errorf("test collect error")
			},
			// We don't want to bubble up the error from the collector, we just want to log it
		},
		"two collectors with no errors": {
			numCollectors: 2,
			collect:       func() error { return nil },
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			c := mock_provider.NewMockCollector(ctrl)
			if tt.collect != nil {

				c.EXPECT().Name().Return("test").AnyTimes()
				c.EXPECT().Collect().DoAndReturn(tt.collect).Times(tt.numCollectors)
			}
			gcp := &GCP{
				config:     &Config{},
				collectors: []provider.Collector{},
			}
			for i := 0; i < tt.numCollectors; i++ {
				gcp.collectors = append(gcp.collectors, c)
			}
			err := gcp.CollectMetrics()
			if tt.expectedError != nil {
				require.EqualError(t, err, tt.expectedError.Error())
				return
			}
			require.NoError(t, err)
		})
	}
}
