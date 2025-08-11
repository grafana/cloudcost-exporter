package cache

type Cache[T any] interface {
	Get(project string) T
	Set(project string, buckets T)
}

type NoopCache[T any] struct{}

func (c NoopCache[T]) Get(_ string) T { var zero T; return zero }

func (c NoopCache[T]) Set(_ string, _ T) {}

func NewNoopCache[T any]() Cache[T] {
	return NoopCache[T]{}
}
