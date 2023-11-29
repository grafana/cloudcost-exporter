package gcs

import (
	"context"
	"testing"

	"cloud.google.com/go/storage"
)

func TestNewBucketClient(t *testing.T) {
	tests := map[string]struct {
		client StorageClientInterface
	}{
		"Empty client": {
			client: &MockStorageClient{},
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

type MockStorageClient struct {
	BucketsFunc func(ctx context.Context, projectID string) *storage.BucketIterator
}

func (m *MockStorageClient) Buckets(ctx context.Context, projectID string) *storage.BucketIterator {
	return m.BucketsFunc(ctx, projectID)
}

func TestBucketClient_List(t *testing.T) {
	mockClient := &MockStorageClient{
		BucketsFunc: func(ctx context.Context, projectID string) *storage.BucketIterator {
			return &storage.BucketIterator{}
		},
	}
	tests := map[string]struct {
		client   *BucketClient
		projects []string
		want     []*storage.BucketAttrs
	}{
		"no projects should result in no results": {
			client:   NewBucketClient(mockClient),
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
