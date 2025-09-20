package kafka

import (
	"context"
	"sync"
)

type ackTracker[K comparable] struct {
	mu        sync.Mutex
	pending   map[K]ackTask
	queueSize int
	queue     chan ackTask
	cancel    context.CancelFunc
	started   bool
	closed    bool
	workers   sync.WaitGroup
}

type ackTask struct {
	ctx context.Context
	fn  func()
}

func newAckTracker[K comparable](size int) *ackTracker[K] {
	if size <= 0 {
		size = 1
	}
	return &ackTracker[K]{
		pending:   make(map[K]ackTask),
		queueSize: size,
	}
}

func (t *ackTracker[K]) Start(parent context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.started {
		return nil
	}
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	queue := make(chan ackTask, t.queueSize)
	t.queue = queue
	t.cancel = cancel
	t.started = true
	t.closed = false

	t.workers.Go(func() {
		for {
			select {
			case <-ctx.Done():
				return
			case task, ok := <-queue:
				if !ok {
					return
				}
				runAckTask(task)
			}
		}
	})
	return nil
}

func (t *ackTracker[K]) Track(id K, ctx context.Context, fn func()) {
	if fn == nil {
		return
	}
	t.mu.Lock()
	t.pending[id] = ackTask{ctx: ctx, fn: fn}
	t.mu.Unlock()
}

func (t *ackTracker[K]) Ack(ctx context.Context, id K) {
	t.mu.Lock()
	task, ok := t.pending[id]
	if ok {
		delete(t.pending, id)
	}
	queue := t.queue
	started := t.started
	t.mu.Unlock()

	if !ok || task.fn == nil {
		return
	}
	if !started || queue == nil {
		runAckTask(task)
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if task.ctx == nil {
		task.ctx = context.Background()
	}

	select {
	case queue <- task:
		return
	default:
	}

	select {
	case <-ctx.Done():
		return
	default:
	}

	select {
	case old := <-queue:
		runAckTask(old)
	default:
	}

	select {
	case queue <- task:
		return
	case <-ctx.Done():
		return
	default:
		runAckTask(task)
	}
}

func (t *ackTracker[K]) Reset() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	dropped := len(t.pending)
	t.pending = make(map[K]ackTask)
	if t.queue != nil {
		for {
			select {
			case <-t.queue:
			default:
				return dropped
			}
		}
	}
	return dropped
}

func (t *ackTracker[K]) Close() {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return
	}
	t.closed = true
	started := t.started
	cancel := t.cancel
	queue := t.queue
	t.started = false
	t.cancel = nil
	t.queue = nil
	t.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if started && queue != nil {
		close(queue)
	}
	t.workers.Wait()
}

func runAckTask(task ackTask) {
	if task.fn == nil {
		return
	}
	ctx := task.ctx
	if ctx != nil {
		select {
		case <-ctx.Done():
			return
		default:
		}
	}
	task.fn()
}
