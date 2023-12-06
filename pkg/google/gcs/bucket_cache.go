package gcs

import (
	"sync"

	"cloud.google.com/go/storage"
)

type BucketCache struct {
	Buckets map[string][]*storage.BucketAttrs
	m       sync.RWMutex
}

func (c *BucketCache) Get(project string) []*storage.BucketAttrs {
	c.m.RLock()
	defer c.m.RUnlock()
	return c.Buckets[project]
}

func (c *BucketCache) Set(project string, buckets []*storage.BucketAttrs) {
	c.m.Lock()
	defer c.m.Unlock()
	c.Buckets[project] = buckets
}

func NewBucketCache() *BucketCache {
	return &BucketCache{
		Buckets: make(map[string][]*storage.BucketAttrs),
		m:       sync.RWMutex{},
	}
}
