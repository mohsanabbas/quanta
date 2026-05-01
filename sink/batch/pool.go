package batch

import "sync"

// Pool manages reusable Batch instances to reduce allocations.
// Optional optimization for high-throughput sinks.
type Pool[T any] struct {
	pool     *sync.Pool
	capacity int
}

// NewPool creates a Pool that produces batches with the given capacity.
func NewPool[T any](capacity int) *Pool[T] {
	if capacity <= 0 {
		capacity = 100
	}
	return &Pool[T]{
		capacity: capacity,
		pool: &sync.Pool{
			New: func() any { return New[T](capacity) },
		},
	}
}

// Get retrieves a batch from the pool (or creates a new one).
func (p *Pool[T]) Get() *Batch[T] {
	return p.pool.Get().(*Batch[T])
}

// Put returns a batch to the pool after resetting it.
func (p *Pool[T]) Put(b *Batch[T]) {
	b.Reset()
	p.pool.Put(b)
}
