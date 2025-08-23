package kafka

import (
	"context"
	"sync"
	"time"
)

type Controller struct {
	capacity int64
	refill   int64

	mu     sync.Mutex
	tokens int64
	cond   *sync.Cond
	closed bool
}

func NewController(cap, refill int64, tick time.Duration) *Controller {
	c := &Controller{
		capacity: cap,
		refill:   refill,
		tokens:   cap,
	}
	c.cond = sync.NewCond(&c.mu)

	go func() {
		t := time.NewTicker(tick)
		for range t.C {
			c.mu.Lock()
			if c.closed {
				c.mu.Unlock()
				return
			}
			c.tokens += c.refill
			if c.tokens > c.capacity {
				c.tokens = c.capacity
			}
			c.mu.Unlock()
			c.cond.Broadcast()
		}
	}()
	return c
}

func (c *Controller) Acquire(ctx context.Context) error {
	c.mu.Lock()
	for c.tokens == 0 && ctx.Err() == nil {
		c.cond.Wait()
	}
	if ctx.Err() != nil {
		c.mu.Unlock()
		return ctx.Err()
	}
	c.tokens--
	c.mu.Unlock()
	return nil
}

func (c *Controller) TryAcquire(n int64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.tokens < n {
		return false
	}
	c.tokens -= n
	return true
}

func (c *Controller) Close() {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	c.cond.Broadcast()
}

func (c *Controller) Release(n int64) {
	c.mu.Lock()
	c.tokens += n
	if c.tokens > c.capacity {
		c.tokens = c.capacity
	}
	c.mu.Unlock()
	c.cond.Broadcast()
}
