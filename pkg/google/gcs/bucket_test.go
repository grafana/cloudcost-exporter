package gcs

import (
	"context"
	"testing"

	"cloud.google.com/go/storage"

	"github.com/grafana/cloudcost-exporter/mocks/pkg/google/gcs"
)

func TestNewBucketClient(t *testing.T) {
	tests := map[string]struct {
		client StorageClientInterface
	}{
		"Empty client": {
			client: gcs.NewStorageClientInterface(t),
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			client := NewBucketClient(test.client)
			if client == nil {
				t.Errorf("expected client to be non-nil")
			}
		})
	}
}

func TestBucketClient_List(t *testing.T) {
	tests := map[string]struct {
		client   *BucketClient
		projects []string
		want     []*storage.BucketAttrs
	}{
		"no projects should result in no results": {
			client:   NewBucketClient(gcs.NewStorageClientInterface(t)),
			projects: []string{},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			for _, project := range test.projects {
				got, err := test.client.List(context.Background(), project)
				if err != nil {
					t.Errorf("unexpected error: %s", err)
				}
				if len(got) != len(test.want) {
					t.Errorf("expected %d buckets, got %d", len(test.want), len(got))
				}
			}

		})
	}
}
