package gcs

import (
	"context"
	"log"

	"cloud.google.com/go/storage"
	"google.golang.org/api/iterator"
)

type StorageClientInterface interface {
	Buckets(ctx context.Context, projectID string) *storage.BucketIterator
}

type BucketClient struct {
	client StorageClientInterface
}

func NewBucketClient(client StorageClientInterface) *BucketClient {
	return &BucketClient{
		client: client,
	}
}

func (bc *BucketClient) list(ctx context.Context, project string) ([]*storage.BucketAttrs, error) {
	var buckets []*storage.BucketAttrs
	log.Printf("Listing buckets for project %s", project)
	it := bc.client.Buckets(ctx, project)
	for {
		bucketAttrs, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		buckets = append(buckets, bucketAttrs)
	}

	return buckets, nil
}

func (bc *BucketClient) List(ctx context.Context, project string) ([]*storage.BucketAttrs, error) {
	return bc.list(ctx, project)
}
