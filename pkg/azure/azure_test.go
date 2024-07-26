package azure

import (
	"context"
	"log/slog"
	"os"
	"testing"

	mock_provider "github.com/grafana/cloudcost-exporter/pkg/provider/mocks"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

var (
	parentCtx  context.Context = context.TODO()
	testLogger *slog.Logger    = slog.New(slog.NewTextHandler(os.Stdout, nil))
)

func Test_New(t *testing.T) {
	testTable := map[string]struct {
		expectedErr error
		subId       string
	}{
		"no subscription ID": {
			expectedErr: InvalidSubscriptionId,
			subId:       "",
		},

		"base case": {
			expectedErr: nil,
			subId:       "asdf-1234",
		},
	}

	for name, tc := range testTable {
		t.Run(name, func(t *testing.T) {
			a, err := New(parentCtx, &Config{
				Logger:         testLogger,
				SubscriptionId: tc.subId,
			})
			if tc.expectedErr != nil {
				assert.ErrorIs(t, err, tc.expectedErr)
				return
			}
			assert.NoError(t, err)
			assert.NotNil(t, a)
		})
	}
}

func Test_RegisterCollectors(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockRegistry := mock_provider.NewMockRegistry(ctrl)

	testCases := map[string]struct {
		mockCollectors []*mock_provider.MockCollector
		expectedErr    error
	}{
		"no collectors": {
			mockCollectors: []*mock_provider.MockCollector{},
		},
		"AKS collector": {
			mockCollectors: []*mock_provider.MockCollector{
				mock_provider.NewMockCollector(ctrl),
			},
		},
		"AKS and future storage collector": {
			mockCollectors: []*mock_provider.MockCollector{
				mock_provider.NewMockCollector(ctrl),
				mock_provider.NewMockCollector(ctrl),
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			azProvider := &Azure{
				logger:  testLogger,
				context: parentCtx,
			}
			for _, c := range tc.mockCollectors {
				call := c.EXPECT().Register(gomock.Any()).AnyTimes()
				call.Return(nil)

				azProvider.collectors = append(azProvider.collectors, c)
			}

			mockRegistry.EXPECT().MustRegister(gomock.Any()).Times(1)
			err := azProvider.RegisterCollectors(mockRegistry)
			assert.Equal(t, err, tc.expectedErr)
		})
	}
}

func Test_CollectMetrics(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ch := make(chan prometheus.Metric)
	testCases := map[string]struct {
		mockCollectors []*mock_provider.MockCollector
		expectedErr    error
	}{
		"base case": {
			mockCollectors: []*mock_provider.MockCollector{
				mock_provider.NewMockCollector(ctrl),
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			go func() {
				// no metrics are generated here, so loop through to avoid
				// process hang waiting on metrics that will never come
				for range ch {
				}
			}()

			azProvider := &Azure{
				logger:  testLogger,
				context: parentCtx,
			}
			for _, c := range tc.mockCollectors {
				c.EXPECT().Collect(gomock.Any()).Times(1)
				c.EXPECT().Name().AnyTimes()

				azProvider.collectors = append(azProvider.collectors, c)
			}

			azProvider.Collect(ch)
		})
	}
}
