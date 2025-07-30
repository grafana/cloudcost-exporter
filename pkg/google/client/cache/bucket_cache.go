package cache

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
	buckets := c.Buckets[project]
	c.m.RUnlock()
	return buckets
}

func (c *BucketCache) Set(project string, buckets []*storage.BucketAttrs) {
	c.m.Lock()
	c.Buckets[project] = buckets
	c.m.Unlock()
}

func NewBucketCache() Cache[[]*storage.BucketAttrs] {
	return &BucketCache{
		Buckets: make(map[string][]*storage.BucketAttrs),
		m:       sync.RWMutex{},
	}
}
