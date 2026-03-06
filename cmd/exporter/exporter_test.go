package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/grafana/cloudcost-exporter/cmd/exporter/config"
	"github.com/grafana/cloudcost-exporter/pkg/aws"
	"github.com/grafana/cloudcost-exporter/pkg/azure"
	"github.com/grafana/cloudcost-exporter/pkg/google"
	"github.com/grafana/cloudcost-exporter/pkg/provider"
	mock_provider "github.com/grafana/cloudcost-exporter/pkg/provider/mocks"
	"go.uber.org/mock/gomock"
)

func Test_selectProvider(t *testing.T) {
	tests := map[string]struct {
		providerName         string
		collectorTimeout     time.Duration
		awsCalled            bool
		azureCalled          bool
		gcpCalled            bool
		constructorErr       error
		wantErr              bool
		wantCollectorTimeout time.Duration // if non-zero, asserts the timeout forwarded to the constructor
	}{
		"aws provider": {
			providerName: "aws",
			awsCalled:    true,
		},
		"azure provider": {
			providerName: "azure",
			azureCalled:  true,
		},
		"gcp provider": {
			providerName: "gcp",
			gcpCalled:    true,
		},
		"aws constructor error is propagated": {
			providerName:   "aws",
			constructorErr: errors.New("constructor failed"),
			wantErr:        true,
		},
		"azure constructor error is propagated": {
			providerName:   "azure",
			constructorErr: errors.New("constructor failed"),
			wantErr:        true,
		},
		"gcp constructor error is propagated": {
			providerName:   "gcp",
			constructorErr: errors.New("constructor failed"),
			wantErr:        true,
		},
		"unknown provider returns error": {
			providerName: "unknown",
			wantErr:      true,
		},
		"zero timeout defaults to one minute": {
			providerName:         "aws",
			collectorTimeout:     0,
			awsCalled:            true,
			wantCollectorTimeout: time.Minute,
		},
		"explicit timeout is passed through": {
			providerName:         "aws",
			collectorTimeout:     5 * time.Minute,
			awsCalled:            true,
			wantCollectorTimeout: 5 * time.Minute,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			var awsCalled, azureCalled, gcpCalled bool
			var capturedTimeout time.Duration

			mockProv := mock_provider.NewMockProvider(ctrl)

			stubAWS := func(_ context.Context, cfg *aws.Config) (provider.Provider, error) {
				awsCalled = true
				capturedTimeout = cfg.CollectorTimeout
				if tc.constructorErr != nil {
					return nil, tc.constructorErr
				}
				return mockProv, nil
			}
			stubAzure := func(_ context.Context, cfg *azure.Config) (provider.Provider, error) {
				azureCalled = true
				capturedTimeout = cfg.CollectorTimeout
				if tc.constructorErr != nil {
					return nil, tc.constructorErr
				}
				return mockProv, nil
			}
			stubGCP := func(_ context.Context, cfg *google.Config) (provider.Provider, error) {
				gcpCalled = true
				capturedTimeout = cfg.CollectorTimeout
				if tc.constructorErr != nil {
					return nil, tc.constructorErr
				}
				return mockProv, nil
			}

			cfg := &config.Config{Provider: tc.providerName}
			cfg.Collector.Timeout = tc.collectorTimeout
			got, err := selectProviderWith(context.Background(), cfg, stubAWS, stubAzure, stubGCP)

			if tc.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got == nil {
				t.Fatal("expected non-nil provider")
			}
			if awsCalled != tc.awsCalled {
				t.Errorf("awsCalled = %v, want %v", awsCalled, tc.awsCalled)
			}
			if azureCalled != tc.azureCalled {
				t.Errorf("azureCalled = %v, want %v", azureCalled, tc.azureCalled)
			}
			if gcpCalled != tc.gcpCalled {
				t.Errorf("gcpCalled = %v, want %v", gcpCalled, tc.gcpCalled)
			}
			if tc.wantCollectorTimeout != 0 && capturedTimeout != tc.wantCollectorTimeout {
				t.Errorf("collectorTimeout = %v, want %v", capturedTimeout, tc.wantCollectorTimeout)
			}
		})
	}
}

func Test_regionFromConfig(t *testing.T) {
	tests := map[string]struct {
		provider  string
		awsRegion string
		gcpRegion string
		want      string
	}{
		"aws returns aws region": {
			provider:  "aws",
			awsRegion: "me-central-1",
			want:      "me-central-1",
		},
		"gcp returns gcp region": {
			provider:  "gcp",
			gcpRegion: "us-central1",
			want:      "us-central1",
		},
		"azure returns empty string": {
			provider: "azure",
			want:     "",
		},
		"unknown provider returns empty string": {
			provider: "unknown",
			want:     "",
		},
		"aws with empty region returns empty string": {
			provider:  "aws",
			awsRegion: "",
			want:      "",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			cfg := &config.Config{Provider: tc.provider}
			cfg.Providers.AWS.Region = tc.awsRegion
			cfg.Providers.GCP.Region = tc.gcpRegion

			got := regionFromConfig(cfg)
			if got != tc.want {
				t.Errorf("regionFromConfig() = %q, want %q", got, tc.want)
			}
		})
	}
}

func Test_createPromRegistryHandler(t *testing.T) {
	tests := map[string]struct {
		setupMock      func(m *mock_provider.MockProvider)
		wantErr        bool
		wantHTTPStatus int
	}{
		"returns error when RegisterCollectors fails": {
			setupMock: func(m *mock_provider.MockProvider) {
				m.EXPECT().Describe(gomock.Any()).AnyTimes()
				m.EXPECT().RegisterCollectors(gomock.Any()).Return(errors.New("collector registration failed"))
			},
			wantErr: true,
		},
		"returns working handler on success": {
			setupMock: func(m *mock_provider.MockProvider) {
				m.EXPECT().Describe(gomock.Any()).AnyTimes()
				m.EXPECT().RegisterCollectors(gomock.Any()).Return(nil)
				m.EXPECT().Collect(gomock.Any()).AnyTimes()
			},
			wantErr:        false,
			wantHTTPStatus: http.StatusOK,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockProv := mock_provider.NewMockProvider(ctrl)
			tc.setupMock(mockProv)

			handler, err := createPromRegistryHandler(mockProv, "us-east-1")

			if tc.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				if handler != nil {
					t.Error("expected nil handler on error")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if handler == nil {
				t.Fatal("expected non-nil handler")
			}

			req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tc.wantHTTPStatus {
				t.Errorf("expected HTTP status %d, got %d", tc.wantHTTPStatus, rec.Code)
			}
		})
	}
}
