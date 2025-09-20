package kafka

import (
	"context"
	"sync"
	"sync/atomic"
)

type ackTracker[K comparable] struct {
	mu      sync.Mutex
	pending map[K]func()
	ch      chan K

	once   sync.Once
	cancel context.CancelFunc
	closed atomic.Bool
}

func newAckTracker[K comparable](size int) *ackTracker[K] {
	if size <= 0 {
		size = 1
	}
	return &ackTracker[K]{
		pending: make(map[K]func()),
		ch:      make(chan K, size),
	}
}

func (t *ackTracker[K]) Start(ctx context.Context) {
	t.once.Do(func() {
		var c context.Context
		c, t.cancel = context.WithCancel(ctx)
		go t.loop(c)
	})
}

func (t *ackTracker[K]) loop(ctx context.Context) {
	defer t.closed.Store(true)
	for {
		select {
		case <-ctx.Done():
			return
		case id := <-t.ch:
			t.exec(id)
		}
	}
}

func (t *ackTracker[K]) exec(id K) {
	t.mu.Lock()
	cb, ok := t.pending[id]
	if ok {
		delete(t.pending, id)
	}
	t.mu.Unlock()
	if ok && cb != nil {
		cb()
	}
}

func (t *ackTracker[K]) Track(id K, fn func()) {
	if fn == nil {
		return
	}
	t.mu.Lock()
	t.pending[id] = fn
	t.mu.Unlock()
}

func (t *ackTracker[K]) Ack(id K) {
	if t.closed.Load() {
		return
	}
	select {
	case t.ch <- id:
	default:
		select {
		case <-t.ch:
		default:
		}
		select {
		case t.ch <- id:
		default:
		}
	}
}

func (t *ackTracker[K]) Reset() int {
	t.mu.Lock()
	dropped := len(t.pending)
	t.pending = make(map[K]func())
	t.mu.Unlock()
	return dropped
}

func (t *ackTracker[K]) Close() {
	if t.cancel != nil {
		t.cancel()
	}
}
