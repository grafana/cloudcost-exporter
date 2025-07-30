package cache

type Cache[T any] interface {
	Get(project string) T
	Set(project string, buckets T)
}

type NoopCache[T any] struct {
	v T
}

func (c NoopCache[T]) Get(_ string) T {
	return c.v
}

func (c NoopCache[T]) Set(_ string, buckets T) {
	c.v = buckets
}

func NewNoopCache[T any]() Cache[T] {
	return NoopCache[T]{}
}
