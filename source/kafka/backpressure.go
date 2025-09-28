package kafka

import (
	"context"

	"golang.org/x/sync/semaphore"
)

type Backpressure struct {
	sem      *semaphore.Weighted
	capacity int64
}

func NewBackpressure(capacity int64) *Backpressure {
	if capacity <= 0 {
		capacity = 1
	}
	return &Backpressure{
		sem:      semaphore.NewWeighted(capacity),
		capacity: capacity,
	}
}

func (b *Backpressure) Acquire(ctx context.Context, n int64) error {
	if n <= 0 {
		n = 1
	}
	return b.sem.Acquire(ctx, n)
}

func (b *Backpressure) Release(n int64) {
	if n <= 0 {
		n = 1
	}
	b.sem.Release(n)
}

func (b *Backpressure) Capacity() int64 {
	return b.capacity
}
