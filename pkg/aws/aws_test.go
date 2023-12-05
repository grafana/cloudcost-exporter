package aws

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/grafana/cloudcost-exporter/pkg/provider"
	mock_provider "github.com/grafana/cloudcost-exporter/pkg/provider/mocks"
)

func Test_New(t *testing.T) {
	for _, tc := range []struct {
		name          string
		expectedError error
	}{
		{
			name: "no error",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// TODO refactor New()
			t.SkipNow()

			a, err := New(&Config{})
			if tc.expectedError != nil {
				require.EqualError(t, err, tc.expectedError.Error())
				return
			}
			require.NoError(t, err)
			require.NotNil(t, a)
		})
	}
}

func Test_RegisterCollectors(t *testing.T) {
	for _, tc := range []struct {
		name          string
		numCollectors int
		register      func(r provider.Registry) error
		expectedError error
	}{
		{
			name: "no error if no collectors",
		},
		{
			name:          "bubble-up single collector error",
			numCollectors: 1,
			register: func(r provider.Registry) error {
				return fmt.Errorf("test register error")
			},
			expectedError: fmt.Errorf("test register error"),
		},
		{
			name:          "two collectors with no errors",
			numCollectors: 2,
			register:      func(r provider.Registry) error { return nil },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			r := mock_provider.NewMockRegistry(ctrl)
			c := mock_provider.NewMockCollector(ctrl)
			if tc.register != nil {
				c.EXPECT().Register(r).DoAndReturn(tc.register).Times(tc.numCollectors)
			}

			a := AWS{
				Config:     nil,
				collectors: []provider.Collector{},
			}
			for i := 0; i < tc.numCollectors; i++ {
				a.collectors = append(a.collectors, c)
			}

			err := a.RegisterCollectors(r)
			if tc.expectedError != nil {
				require.EqualError(t, err, tc.expectedError.Error())
				return
			}
			require.NoError(t, err)
		})
	}
}

func Test_CollectMetrics(t *testing.T) {
	for _, tc := range []struct {
		name          string
		numCollectors int
		collect       func() error
		expectedError error
	}{
		{
			name: "no error if no collectors",
		},
		{
			name:          "bubble-up single collector error",
			numCollectors: 1,
			collect: func() error {
				return fmt.Errorf("test collect error")
			},
			expectedError: fmt.Errorf("test collect error"),
		},
		{
			name:          "two collectors with no errors",
			numCollectors: 2,
			collect:       func() error { return nil },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			c := mock_provider.NewMockCollector(ctrl)
			if tc.collect != nil {
				c.EXPECT().Collect().DoAndReturn(tc.collect).Times(tc.numCollectors)
			}

			a := AWS{
				Config:     nil,
				collectors: []provider.Collector{},
			}
			for i := 0; i < tc.numCollectors; i++ {
				a.collectors = append(a.collectors, c)
			}

			err := a.CollectMetrics()
			if tc.expectedError != nil {
				require.EqualError(t, err, tc.expectedError.Error())
				return
			}
			require.NoError(t, err)
		})
	}
}
