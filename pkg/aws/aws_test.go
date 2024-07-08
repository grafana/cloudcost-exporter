package aws

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/grafana/cloudcost-exporter/pkg/provider"
	mock_provider "github.com/grafana/cloudcost-exporter/pkg/provider/mocks"
)

var logger = slog.New(slog.NewTextHandler(os.Stdout, nil))

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

			a, err := New(context.Background(), &Config{})
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
			r.EXPECT().MustRegister(gomock.Any()).AnyTimes()
			c := mock_provider.NewMockCollector(ctrl)
			if tc.register != nil {
				c.EXPECT().Register(r).DoAndReturn(tc.register).Times(tc.numCollectors)
			}

			a := AWS{
				Config:     nil,
				collectors: []provider.Collector{},
				logger:     logger,
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
		collect       func(chan<- prometheus.Metric) error
	}{
		{
			name: "no error if no collectors",
		},
		{
			name:          "bubble-up single collector error",
			numCollectors: 1,
			collect: func(chan<- prometheus.Metric) error {
				return nil
			},
		},
		{
			name:          "two collectors with no errors",
			numCollectors: 2,
			collect:       func(chan<- prometheus.Metric) error { return nil },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ch := make(chan prometheus.Metric)
			go func() {
				for range ch {
					// This is necessary to ensure the test doesn't hang
				}
			}()
			ctrl := gomock.NewController(t)
			c := mock_provider.NewMockCollector(ctrl)
			if tc.collect != nil {
				c.EXPECT().Collect(ch).DoAndReturn(tc.collect).Times(tc.numCollectors)
				c.EXPECT().Name().Return("test").AnyTimes()
			}

			a := AWS{
				Config:     nil,
				collectors: []provider.Collector{},
				logger:     logger,
			}
			for i := 0; i < tc.numCollectors; i++ {
				a.collectors = append(a.collectors, c)
			}

			a.Collect(ch)
			close(ch)
		})
	}
}
