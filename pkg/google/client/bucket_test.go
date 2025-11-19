package client

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"cloud.google.com/go/storage"
	"github.com/grafana/cloudcost-exporter/pkg/google/client/cache"
	mock_client "github.com/grafana/cloudcost-exporter/pkg/google/client/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/api/option"
)

func TestNewBucketClient(t *testing.T) {
	tests := map[string]struct {
		client StorageClientInterface
	}{
		"Empty cloudCatalogClient": {
			client: mock_client.NewMockStorageClientInterface(gomock.NewController(t)),
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			client := newBucket(test.client, cache.NewNoopCache[[]*storage.BucketAttrs]())
			if client == nil {
				t.Errorf("expected cloudCatalogClient to be non-nil")
			}
		})
	}
}

// note: not checking this error because we don't care if w.Write() fails,
// that's not our code to fix :)
//
//nolint:errcheck
func TestBucketClient_List(t *testing.T) {
	tests := map[string]struct {
		server   *httptest.Server
		projects []string
		want     int
		wantErr  bool
	}{
		"no projects should result in no results": {
			projects: []string{"project-1"},
			server: httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"items": []}`))
			})),
			want:    0,
			wantErr: false,
		},
		"one item should result in one bucket": {
			projects: []string{"project-1"},
			server: httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"items": [{"name": "testing-123"}]}`))
			})),
			want:    1,
			wantErr: false,
		},
		"An error should be handled": {
			projects: []string{"project-1"},
			server: httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(``))
			},
			)),
			want:    0,
			wantErr: true,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			for _, project := range test.projects {
				sc, err := storage.NewClient(t.Context(), option.WithEndpoint(test.server.URL), option.WithAPIKey("hunter2"))
				require.NoError(t, err)
				bc := newBucket(sc, cache.NewNoopCache[[]*storage.BucketAttrs]())
				got, err := bc.List(t.Context(), project)
				assert.Equal(t, test.wantErr, err != nil)
				assert.NotNil(t, got)
				assert.Equal(t, test.want, len(got))
			}
		})
	}
}
