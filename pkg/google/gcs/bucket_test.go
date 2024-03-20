package gcs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"cloud.google.com/go/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/option"

	"github.com/grafana/cloudcost-exporter/mocks/pkg/google/gcs"
)

func TestNewBucketClient(t *testing.T) {
	tests := map[string]struct {
		client StorageClientInterface
	}{
		"Empty cloudCatalogClient": {
			client: gcs.NewStorageClientInterface(t),
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			client := NewBucketClient(test.client)
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
				sc, err := storage.NewClient(context.Background(), option.WithEndpoint(test.server.URL), option.WithAPIKey("hunter2"))
				require.NoError(t, err)
				bc := NewBucketClient(sc)
				got, err := bc.List(context.Background(), project)
				assert.Equal(t, test.wantErr, err != nil)
				assert.NotNil(t, got)
				assert.Equal(t, test.want, len(got))
			}
		})
	}
}
