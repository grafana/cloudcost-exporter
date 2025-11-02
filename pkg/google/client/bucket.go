package client

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"cloud.google.com/go/storage"
	"github.com/grafana/cloudcost-exporter/pkg/google/client/cache"
	"github.com/grafana/cloudcost-exporter/pkg/google/metrics"
	"google.golang.org/api/iterator"
)

//go:generate mockgen -source=bucket.go -destination mocks/bucket.go

type StorageClientInterface interface {
	Buckets(ctx context.Context, projectID string) *storage.BucketIterator
}

type Bucket struct {
	cache         cache.Cache[[]*storage.BucketAttrs]
	storageClient StorageClientInterface
}

func newBucket(storageClient StorageClientInterface, cache cache.Cache[[]*storage.BucketAttrs]) *Bucket {
	return &Bucket{
		cache:         cache,
		storageClient: storageClient,
	}
}

func (b *Bucket) List(ctx context.Context, project string) ([]*storage.BucketAttrs, error) {
	log.Printf("Listing buckets for project %s", project)
	buckets := make([]*storage.BucketAttrs, 0)
	it := b.storageClient.Buckets(ctx, project)
	for {
		bucketAttrs, err := it.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return buckets, err
		}
		buckets = append(buckets, bucketAttrs)
	}

	return buckets, nil
}

// exportBucketInfo will list all buckets for a given project and export the data as a prometheus metric.
// If there are any errors listing buckets, it will export the cached buckets for the project.
func (b *Bucket) exportBucketInfo(ctx context.Context, projects []string, m *metrics.Metrics) error {
	var buckets []*storage.BucketAttrs
	for _, project := range projects {
		start := time.Now()

		var err error
		buckets, err = b.List(ctx, project)
		if err != nil {
			// We don't want to block here as it's not critical to the exporter
			log.Printf("error listing buckets for %s: %v", project, err)
			m.BucketListHistogram.WithLabelValues(project).Observe(time.Since(start).Seconds())
			m.BucketListStatus.WithLabelValues(project, "error").Inc()
			buckets = b.cache.Get(project)
			log.Printf("pulling %d cached buckets for project %s", len(buckets), project)
		}

		log.Printf("updating cached buckets for %s", project)
		b.cache.Set(project, buckets)

		for _, bucket := range buckets {
			// Location is always in caps, and the metrics that need to join up on it are in lower case
			m.BucketInfo.WithLabelValues(strings.ToLower(bucket.Location), bucket.LocationType, bucket.StorageClass, bucket.Name).Set(1)
		}
		m.BucketListHistogram.WithLabelValues(project).Observe(time.Since(start).Seconds())
		m.BucketListStatus.WithLabelValues(project, "success").Inc()
	}

	return nil
}
