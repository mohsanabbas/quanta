package kafka

import (
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

func (c *SlidingWindowCheckpointManager) Track(offset int64, size int64) error {
	if size <= 0 {
		size = 1
	}

	backoff := _spinBackoff
	for attempt := 0; attempt < _maxSpinAttempts; attempt++ {
		if c.tracker.Reserve(offset) != InvalidSlot {
			c.acker.Track(offset, AckHandle{offset: offset, bytes: size})
			return nil
		}
		time.Sleep(backoff)
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
	cond        *sync.Cond
	capacity    int64
	pending     map[int64]AckHandle
	baseOffset  int64
	initialized bool
	closed      bool
}

func NewApplicationControlledCheckpointManager(capacity int64) *ApplicationControlledCheckpointManager {
	if capacity <= 0 {
		capacity = 1000
	}
	mgr := &ApplicationControlledCheckpointManager{
		capacity:    capacity,
		pending:     make(map[int64]AckHandle),
		baseOffset:  -1,
		initialized: false,
	}
	mgr.cond = sync.NewCond(&mgr.mu)
	return mgr
}

func (c *ApplicationControlledCheckpointManager) Track(offset int64, size int64) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	for !c.closed && int64(len(c.pending)) >= c.capacity {
		c.cond.Wait()
	}
	if c.closed {
		return ErrCheckpointClosed
	}
	if size <= 0 {
		size = 1
	}

	if !c.initialized {
		c.baseOffset = offset
		c.initialized = true
	}

	c.pending[offset] = AckHandle{
		offset: offset,
		bytes:  size,
	}
	return nil
}

func (c *ApplicationControlledCheckpointManager) Ack(offset int64) (AckHandle, int64, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	handle, ok := c.pending[offset]
	if !ok {
		return AckHandle{}, c.baseOffset, false
	}
	delete(c.pending, offset)
	c.cond.Broadcast()

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
	return handle, c.baseOffset, advanced
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
	defer c.mu.Unlock()

	handles := make([]AckHandle, 0, len(c.pending))
	for _, h := range c.pending {
		handles = append(handles, h)
	}
	c.pending = make(map[int64]AckHandle)
	c.baseOffset = -1
	c.initialized = false
	c.cond.Broadcast()
	return handles
}

func (c *ApplicationControlledCheckpointManager) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	c.pending = nil
	c.cond.Broadcast()
}
