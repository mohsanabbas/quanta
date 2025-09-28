package kafka

import "context"

type Controller struct {
	ch chan struct{}
}

func NewController(capacity int64) *Controller {
	if capacity <= 0 {
		capacity = 1
	}
	return &Controller{ch: make(chan struct{}, capacity)}
}

func (c *Controller) Acquire(ctx context.Context) error {
	select {
	case c.ch <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *Controller) TryAcquire() bool {
	select {
	case c.ch <- struct{}{}:
		return true
	default:
		return false
	}
}

func (c *Controller) Release(n int64) {
	for i := int64(0); i < n; i++ {
		select {
		case <-c.ch:
		default:
			return
		}
	}
}

func (c *Controller) Close() {
	// Nothing to do; channel garbage collects once controller released.
}
