package kafka

import (
	"context"
	"errors"
	"sync"
	"time"
)

var ErrCheckpointClosed = errors.New("kafka: checkpoint manager closed")

const (
	_defaultCapacity = 1024
	_spinBackoff     = 200 * time.Microsecond
	_maxSpinBackoff  = 10 * time.Millisecond
	_maxSpinAttempts = 500
)

var ErrWindowExhausted = errors.New("kafka: sliding window exhausted after max attempts")

type SlidingWindowCheckpointManager struct {
	tracker *PartitionTracker
	acker   *OffsetTracker
}

func NewSlidingWindowCheckpointManager(windowBits uint32, capacity int) *SlidingWindowCheckpointManager {
	if windowBits < _minWindowBits {
		windowBits = _defaultWindow
	}
	if capacity <= 0 {
		capacity = _defaultCapacity
	}
	return &SlidingWindowCheckpointManager{
		tracker: NewPartitionTracker(windowBits),
		acker:   NewOffsetTracker(capacity),
	}
}

func (c *SlidingWindowCheckpointManager) Track(ctx context.Context, offset int64, size int64) error {
	if size <= 0 {
		size = 1
	}

	backoff := _spinBackoff
	for range _maxSpinAttempts {
		if c.tracker.Reserve(offset) != InvalidSlot {
			c.acker.Track(offset, AckHandle{offset: offset, bytes: size})
			return nil
		}
		// Honour ctx cancellation during the back-off so shutdown cannot
		// wedge waiting up to the spin budget.
		if ctx != nil {
			timer := time.NewTimer(backoff)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			}
		} else {
			time.Sleep(backoff)
		}
		backoff *= 2
		if backoff > _maxSpinBackoff {
			backoff = _maxSpinBackoff
		}
	}
	return ErrWindowExhausted
}

func (c *SlidingWindowCheckpointManager) Ack(offset int64) (AckHandle, int64, bool) {
	handle, ok := c.acker.Ack(offset)
	if !ok {
		newBase, advanced := c.tracker.AckOffset(offset)
		return AckHandle{}, newBase, advanced
	}

	newBase, advanced := c.tracker.AckOffset(offset)
	return handle, newBase, advanced
}

func (c *SlidingWindowCheckpointManager) Base() int64 {
	return c.tracker.Base()
}

func (c *SlidingWindowCheckpointManager) Initialized() bool {
	return c.tracker.Initialized()
}

func (c *SlidingWindowCheckpointManager) Reset() []AckHandle {
	return c.acker.Reset()
}

func (c *SlidingWindowCheckpointManager) Close() {
	c.acker.Close()
}

type ApplicationControlledCheckpointManager struct {
	mu          sync.Mutex
	capacity    int64
	pending     map[int64]AckHandle
	baseOffset  int64
	initialized bool
	closed      bool
	notify      chan struct{} // buffered cap 1: kick parked Track callers
	closeCh     chan struct{} // closed by Close
	closeOnce   sync.Once
}

func NewApplicationControlledCheckpointManager(capacity int64) *ApplicationControlledCheckpointManager {
	if capacity <= 0 {
		capacity = 1000
	}
	return &ApplicationControlledCheckpointManager{
		capacity:    capacity,
		pending:     make(map[int64]AckHandle),
		baseOffset:  -1,
		initialized: false,
		notify:      make(chan struct{}, 1),
		closeCh:     make(chan struct{}),
	}
}

// kick sends a non-blocking notify to wake one parked Track caller.
func (c *ApplicationControlledCheckpointManager) kick() {
	select {
	case c.notify <- struct{}{}:
	default:
	}
}

func (c *ApplicationControlledCheckpointManager) Track(ctx context.Context, offset int64, size int64) error {
	if size <= 0 {
		size = 1
	}
	// Loop re-checks capacity under the lock each iteration. This makes the
	// cap-1 `notify` channel safe to be lossy: a missed wakeup only delays
	// the next attempt, it cannot deadlock because the capacity check runs
	// again before we re-park.
	for {
		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			return ErrCheckpointClosed
		}
		if int64(len(c.pending)) < c.capacity {
			if !c.initialized {
				c.baseOffset = offset
				c.initialized = true
			}
			c.pending[offset] = AckHandle{offset: offset, bytes: size}
			c.mu.Unlock()
			return nil
		}
		c.mu.Unlock()

		// Block until capacity, ctx cancellation, or close.
		if ctx == nil {
			select {
			case <-c.notify:
			case <-c.closeCh:
				return ErrCheckpointClosed
			}
			continue
		}
		select {
		case <-c.notify:
		case <-c.closeCh:
			return ErrCheckpointClosed
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (c *ApplicationControlledCheckpointManager) Ack(offset int64) (AckHandle, int64, bool) {
	c.mu.Lock()
	handle, ok := c.pending[offset]
	if !ok {
		base := c.baseOffset
		c.mu.Unlock()
		return AckHandle{}, base, false
	}
	delete(c.pending, offset)

	oldBase := c.baseOffset
	if len(c.pending) == 0 {
		c.baseOffset = offset + 1
	} else {
		minOffset := offset + 1
		for o := range c.pending {
			if o < minOffset {
				minOffset = o
			}
		}
		c.baseOffset = minOffset
	}
	advanced := c.baseOffset > oldBase
	base := c.baseOffset
	c.mu.Unlock()

	c.kick()
	return handle, base, advanced
}

func (c *ApplicationControlledCheckpointManager) Base() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.baseOffset
}

func (c *ApplicationControlledCheckpointManager) Initialized() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.initialized
}

func (c *ApplicationControlledCheckpointManager) Reset() []AckHandle {
	c.mu.Lock()
	handles := make([]AckHandle, 0, len(c.pending))
	for _, h := range c.pending {
		handles = append(handles, h)
	}
	c.pending = make(map[int64]AckHandle)
	c.baseOffset = -1
	c.initialized = false
	c.mu.Unlock()

	c.kick()
	return handles
}

func (c *ApplicationControlledCheckpointManager) Close() {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closed = true
		c.pending = nil
		c.mu.Unlock()
		close(c.closeCh)
	})
}
