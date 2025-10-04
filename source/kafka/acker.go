package kafka

import "sync"

type AckHandle struct {
	offset int64
	bytes  int64
}

type OffsetTracker struct {
	mu   sync.Mutex
	m    map[int64]AckHandle
	open bool
}

func NewOffsetTracker(capacity int) *OffsetTracker {
	if capacity <= 0 {
		capacity = 1024
	}
	return &OffsetTracker{
		m:    make(map[int64]AckHandle, capacity),
		open: true,
	}
}

func (ot *OffsetTracker) Track(offset int64, h AckHandle) {
	ot.mu.Lock()
	if ot.open {
		ot.m[offset] = h
	}
	ot.mu.Unlock()
}

func (ot *OffsetTracker) Ack(offset int64) (AckHandle, bool) {
	ot.mu.Lock()
	h, ok := ot.m[offset]
	if ok {
		delete(ot.m, offset)
	}
	ot.mu.Unlock()
	return h, ok
}

func (ot *OffsetTracker) Remove(offset int64) (AckHandle, bool) {
	ot.mu.Lock()
	h, ok := ot.m[offset]
	if ok {
		delete(ot.m, offset)
	}
	ot.mu.Unlock()
	return h, ok
}

func (ot *OffsetTracker) Reset() []AckHandle {
	ot.mu.Lock()
	defer ot.mu.Unlock()
	handles := make([]AckHandle, 0, len(ot.m))
	for _, h := range ot.m {
		handles = append(handles, h)
	}
	ot.m = make(map[int64]AckHandle, cap(handles))
	return handles
}

func (ot *OffsetTracker) Close() {
	ot.mu.Lock()
	ot.open = false
	ot.m = nil
	ot.mu.Unlock()
}

func (ot *OffsetTracker) Size() int {
	ot.mu.Lock()
	defer ot.mu.Unlock()
	return len(ot.m)
}
