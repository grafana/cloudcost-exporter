package cache

import (
	"strconv"
	"testing"

	"cloud.google.com/go/storage"
	"github.com/stretchr/testify/assert"
)

func TestBucketCache_Get(t *testing.T) {
	tests := map[string]struct {
		want     int
		error    assert.ErrorAssertionFunc
		projects []string
		buckets  []*storage.BucketAttrs
	}{
		"empty": {
			want:  0,
			error: assert.Error,
		},
		"one": {
			want:     1,
			projects: []string{"test"},
			buckets:  generateNBuckets(1),
		},
		"ten": {
			want:     10,
			projects: []string{"test"},
			buckets:  generateNBuckets(10),
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			bucketCache := NewBucketCache()
			for _, project := range tt.projects {
				bucketCache.Set(project, tt.buckets)
			}
			for _, project := range tt.projects {
				buckets := bucketCache.Get(project)
				assert.Equal(t, tt.want, len(buckets))
			}
		})
	}
}

func BenchmarkBucketCache_Get(b *testing.B) {
	bucketCache := NewBucketCache()
	buckets := generateNBuckets(1000)
	bucketCache.Set("test", buckets)
	for i := 0; i < b.N; i++ {
		bucketCache.Get("test")
	}
}

func BenchmarkBucketCache_GetParallel(b *testing.B) {
	bucketCache := NewBucketCache()
	buckets := generateNBuckets(1000)
	bucketCache.Set("test", buckets)
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			bucketCache.Get("test")
		}
	})
}

func BenchmarkBucketCache_Set(b *testing.B) {
	bucketCache := NewBucketCache()
	buckets := generateNBuckets(1000)
	for i := 0; i < b.N; i++ {
		bucketCache.Set(strconv.Itoa(i), buckets)
	}
}

func BenchmarkBucketCache_SetParallel(b *testing.B) {
	bucketCache := NewBucketCache()
	buckets := generateNBuckets(1000)
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			bucketCache.Set("test", buckets)
		}
	})
}

func generateNBuckets(n int) []*storage.BucketAttrs {
	buckets := make([]*storage.BucketAttrs, n)
	for i := 0; i < n; i++ {
		buckets[i] = &storage.BucketAttrs{
			Name: "test",
		}
	}
	return buckets
}
