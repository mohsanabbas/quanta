package kafka

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

/* ───────────────────────── Uncapped & Capped ───────────────────────────── */

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

func NewUncapped[T any]() *Uncapped[T] { return &Uncapped[T]{} }

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
func (u *Uncapped[T]) Highest() *T { return u.cpPay }

type Capped[T any] struct {
	u    *Uncapped[T]
	cap  int64
	cond *sync.Cond
}

func NewCapped[T any](cap int64) *Capped[T] {
	return &Capped[T]{u: NewUncapped[T](), cap: cap, cond: sync.NewCond(&sync.Mutex{})}
}

// Track blocks when pending+batch exceeds cap.
func (c *Capped[T]) Track(ctx context.Context, p T, batch int64) (func() *T, error) {
	c.cond.L.Lock()
	defer c.cond.L.Unlock()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		<-ctx.Done()
		c.cond.Broadcast()
	}()

	for pend := c.u.Pending(); pend > 0 && pend+batch > c.cap; pend = c.u.Pending() {
		c.cond.Wait()
		if err := ctx.Err(); err != nil {
			return nil, errors.New("checkpoint track context error")
		}
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
	return c.u.Pending()
}

func (c *Capped[T]) Highest() *T {
	return c.u.Highest()
}

/* ───────────────────────── Manager (commit helper) ────────────────────── */

// Manager decides *when* a driver should flush its offsets.
type Manager[T any] struct {
	capped        *Capped[T]
	commitEveryNS int64
	lastCommitNS  int64
}

func NewManager[T any](cap int64, commitEvery time.Duration) *Manager[T] {
	return &Manager[T]{
		capped:        NewCapped[T](cap),
		commitEveryNS: commitEvery.Nanoseconds(),
	}
}

// Track returns (resolveFn, err).
// After the driver has successfully emitted the payload, it must call the
// returned resolveFn(), which indicates whether a commit is now due.
func (m *Manager[T]) Track(ctx context.Context, payload T) (resolveFn func() (highest *T, shouldCommit bool), err error) {
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
