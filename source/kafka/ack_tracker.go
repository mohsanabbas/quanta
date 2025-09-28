package kafka

import (
	"context"
	"sync"
)

type ackTracker[K comparable] struct {
	mu      sync.Mutex
	pending map[K]func()
}

func newAckTracker[K comparable]() *ackTracker[K] {
	return &ackTracker[K]{pending: make(map[K]func())}
}

func (t *ackTracker[K]) Start(context.Context) error { return nil }

func (t *ackTracker[K]) Track(id K, fn func()) {
	if fn == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.pending == nil {
		t.pending = make(map[K]func())
	}
	t.pending[id] = fn
}

func (t *ackTracker[K]) Cancel(id K) {
	t.mu.Lock()
	if t.pending != nil {
		delete(t.pending, id)
	}
	t.mu.Unlock()
}

func (t *ackTracker[K]) Ack(id K) bool {
	t.mu.Lock()
	fn, ok := t.pending[id]
	if ok {
		delete(t.pending, id)
	}
	t.mu.Unlock()
	if ok {
		fn()
	}
	return ok
}

func (t *ackTracker[K]) Reset() int {
	t.mu.Lock()
	count := len(t.pending)
	t.pending = make(map[K]func())
	t.mu.Unlock()
	return count
}

func (t *ackTracker[K]) Close() {
	t.mu.Lock()
	t.pending = nil
	t.mu.Unlock()
}
