package kafka

import (
	"context"

	qerr "quanta/internal/errors"

	"golang.org/x/sync/semaphore"
)

const (
	_defaultMaxMsgs = 1000
)

type CombinedBackpressureManager struct {
	byteSem  *semaphore.Weighted
	msgSem   *semaphore.Weighted
	bytesCap int64
	msgsCap  int64
}

func NewCombinedBackpressureManager(maxBytes int64, maxMsgs int64) *CombinedBackpressureManager {
	if maxBytes <= 0 {
		maxBytes = _defaultMaxBytes
	}
	if maxMsgs <= 0 {
		maxMsgs = _defaultMaxMsgs
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

	if err := b.byteSem.Acquire(ctx, size); err != nil {
		return qerr.Source("kafka", "backpressure", err)
	}

	if err := b.msgSem.Acquire(ctx, 1); err != nil {
		b.byteSem.Release(size)
		return qerr.Source("kafka", "backpressure", err)
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

func (b *CountBasedBackpressureManager) Acquire(ctx context.Context, _ int64) error {
	return b.sem.Acquire(ctx, 1)
}

func (b *CountBasedBackpressureManager) Release(_ int64) {
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
		maxBytes = _defaultMaxBytes
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
