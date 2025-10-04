package kafka

import (
	"context"
	"fmt"

	"golang.org/x/sync/semaphore"
)

type CombinedBackpressureManager struct {
	byteSem  *semaphore.Weighted
	msgSem   *semaphore.Weighted
	bytesCap int64
	msgsCap  int64
}

func NewCombinedBackpressureManager(maxBytes int64, maxMsgs int64) *CombinedBackpressureManager {
	if maxBytes <= 0 {
		maxBytes = 1024 * 1024 * 100 // 100MB default
	}
	if maxMsgs <= 0 {
		maxMsgs = 1000
	}
	return &CombinedBackpressureManager{
		byteSem:  semaphore.NewWeighted(maxBytes),
		msgSem:   semaphore.NewWeighted(maxMsgs),
		bytesCap: maxBytes,
		msgsCap:  maxMsgs,
	}
}

func (b *CombinedBackpressureManager) Acquire(ctx context.Context, size int64) error {
	if size <= 0 {
		size = 1
	}
	// First acquire byte token
	if err := b.byteSem.Acquire(ctx, size); err != nil {
		return fmt.Errorf("acquire byte token: %w", err)
	}
	// Then acquire a message token
	if err := b.msgSem.Acquire(ctx, 1); err != nil {
		b.byteSem.Release(size)
		return fmt.Errorf("acquire message token: %w", err)
	}
	return nil
}

func (b *CombinedBackpressureManager) Release(size int64) {
	if size <= 0 {
		size = 1
	}
	b.msgSem.Release(1)
	b.byteSem.Release(size)
}

func (b *CombinedBackpressureManager) Capacity() int64 {
	return b.bytesCap
}

type CountBasedBackpressureManager struct {
	sem      *semaphore.Weighted
	capacity int64
}

func NewCountBasedBackpressureManager(maxCount int64) *CountBasedBackpressureManager {
	if maxCount <= 0 {
		maxCount = 1000
	}
	return &CountBasedBackpressureManager{
		sem:      semaphore.NewWeighted(maxCount),
		capacity: maxCount,
	}
}

func (b *CountBasedBackpressureManager) Acquire(ctx context.Context, size int64) error {
	return b.sem.Acquire(ctx, 1)
}

func (b *CountBasedBackpressureManager) Release(size int64) {
	b.sem.Release(1)
}

func (b *CountBasedBackpressureManager) Capacity() int64 {
	return b.capacity
}

type SizeBasedBackpressureManager struct {
	sem      *semaphore.Weighted
	capacity int64
}

func NewSizeBasedBackpressureManager(maxBytes int64) *SizeBasedBackpressureManager {
	if maxBytes <= 0 {
		maxBytes = 1024 * 1024 * 100 // 100MB default
	}
	return &SizeBasedBackpressureManager{
		sem:      semaphore.NewWeighted(maxBytes),
		capacity: maxBytes,
	}
}

func (b *SizeBasedBackpressureManager) Acquire(ctx context.Context, size int64) error {
	if size <= 0 {
		size = 1
	}
	return b.sem.Acquire(ctx, size)
}

func (b *SizeBasedBackpressureManager) Release(size int64) {
	if size <= 0 {
		size = 1
	}
	b.sem.Release(size)
}

func (b *SizeBasedBackpressureManager) Capacity() int64 {
	return b.capacity
}
