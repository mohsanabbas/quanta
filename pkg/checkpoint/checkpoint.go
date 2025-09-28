package checkpoint

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

type node[T any] struct {
	pos        int64
	payload    T
	prev, next *node[T]
}

type Uncapped[T any] struct {
	cpPos      int64
	cpPay      *T
	start, end *node[T]
}

func NewUncapped[T any]() *Uncapped[T] {
	return &Uncapped[T]{}
}

func (u *Uncapped[T]) Track(p T, size int64) func() *T {
	n := &node[T]{payload: p, pos: size}
	if u.start == nil {
		u.start = n
	}
	if u.end != nil {
		n.prev = u.end
		n.pos += u.end.pos
		u.end.next = n
	} else {
		n.pos += u.cpPos
	}
	u.end = n
	return func() *T {
		if n.prev != nil {
			n.prev.pos = n.pos
			n.prev.payload = n.payload
			n.prev.next = n.next
		} else {
			tmp := n.payload
			u.cpPay, u.cpPos = &tmp, n.pos
			u.start = n.next
		}
		if n.next != nil {
			n.next.prev = n.prev
		} else {
			u.end = n.prev
		}
		return u.cpPay
	}
}

func (u *Uncapped[T]) Pending() int64 {
	if u.end == nil {
		return 0
	}
	return u.end.pos - u.cpPos
}

func (u *Uncapped[T]) Highest() *T {
	return u.cpPay
}

// Capped - Bounded checkpoint tracker with thread safety
type Capped[T any] struct {
	u    *Uncapped[T]
	cap  int64
	cond *sync.Cond
}

func NewCapped[T any](capacity int64) *Capped[T] {
	if capacity <= 0 {
		capacity = 1
	}
	return &Capped[T]{u: NewUncapped[T](), cap: capacity, cond: sync.NewCond(&sync.Mutex{})}
}

func (c *Capped[T]) Track(ctx context.Context, p T, batch int64) (func() *T, error) {
	c.cond.L.Lock()
	defer c.cond.L.Unlock()
	for pend := c.u.Pending(); pend > 0 && pend+batch > c.cap; pend = c.u.Pending() {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		c.cond.Wait()
	}
	res := c.u.Track(p, batch)
	return func() *T {
		c.cond.L.Lock()
		defer c.cond.L.Unlock()
		r := res()
		c.cond.Broadcast()
		return r
	}, nil
}

func (c *Capped[T]) Pending() int64 {
	c.cond.L.Lock()
	defer c.cond.L.Unlock()
	return c.u.Pending()
}

func (c *Capped[T]) Highest() *T {
	c.cond.L.Lock()
	defer c.cond.L.Unlock()
	return c.u.Highest()
}

// Manager - Checkpoint manager with commit timing
type Manager[T any] struct {
	capped        *Capped[T]
	commitEveryNS int64
	lastCommitNS  int64
}

func NewManager[T any](capacity int64, commitEvery time.Duration) *Manager[T] {
	return &Manager[T]{
		capped:        NewCapped[T](capacity),
		commitEveryNS: commitEvery.Nanoseconds(),
	}
}

func (m *Manager[T]) Track(ctx context.Context, payload T) (resolve func() (*T, bool), err error) {
	res, err := m.capped.Track(ctx, payload, 1)
	if err != nil {
		return nil, err
	}
	return func() (*T, bool) {
		highest := res()
		now := time.Now().UnixNano()
		if atomic.LoadInt64(&m.lastCommitNS)+m.commitEveryNS <= now {
			atomic.StoreInt64(&m.lastCommitNS, now)
			return highest, true
		}
		return highest, false
	}, nil
}

func (m *Manager[T]) Reset(capacity int64) {
	m.capped = NewCapped[T](capacity)
	atomic.StoreInt64(&m.lastCommitNS, 0)
}
