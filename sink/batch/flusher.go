package batch

import (
	"context"
	"log/slog"
	"sync"
	"time"

	pb "quanta/api/proto/v1"
	"quanta/sink"
)

// FlushFunc persists sealed records. Must be atomic.
type FlushFunc[T any] func(ctx context.Context, records []Record[T]) error

// Callbacks wraps ack/nack functions for delivery confirmation.
type Callbacks struct {
	Ack  sink.EmitFn
	Nack sink.NackFn
}

// FlusherConfig controls batching behavior.
type FlusherConfig struct {
	BatchSize     int
	FlushInterval time.Duration
}

const (
	_defaultBatchSize     = 100
	_defaultFlushInterval = 5 * time.Second
)

func (c *FlusherConfig) setDefaults() {
	if c.BatchSize <= 0 {
		c.BatchSize = _defaultBatchSize
	}
	if c.FlushInterval <= 0 {
		c.FlushInterval = _defaultFlushInterval
	}
}

// Flusher manages batch lifecycle
type Flusher[T any] struct {
	batch     *Batch[T]
	flushFn   FlushFunc[T]
	callbacks Callbacks
	cfg       FlusherConfig

	mu        sync.Mutex
	sealCh    chan []Record[T] // buffer=1 for backpressure
	stopCh    chan struct{}
	doneCh    chan struct{}
	closeOnce sync.Once
	wg        sync.WaitGroup
}

// NewFlusher creates a Flusher. Call Start to begin the flush loop.
func NewFlusher[T any](cfg FlusherConfig, flushFn FlushFunc[T], cb Callbacks) *Flusher[T] {
	cfg.setDefaults()
	return &Flusher[T]{
		batch:     New[T](cfg.BatchSize),
		flushFn:   flushFn,
		callbacks: cb,
		cfg:       cfg,
		sealCh:    make(chan []Record[T], 1),
		stopCh:    make(chan struct{}),
		doneCh:    make(chan struct{}),
	}
}

// Start begins the flush loop goroutine.
func (f *Flusher[T]) Start(ctx context.Context) {
	f.wg.Go(func() {
		f.flushLoop(ctx)
	})
}

// Add appends a record. Seals and sends to flush loop when batch is full.
func (f *Flusher[T]) Add(ctx context.Context, data T, cp *pb.CheckpointToken, frame *pb.Frame, byteSize int64) error {
	f.mu.Lock()

	select {
	case <-f.stopCh:
		f.mu.Unlock()
		return ErrFlusherClosed
	default:
	}

	full := f.batch.Append(data, cp, frame, byteSize)
	if !full {
		f.mu.Unlock()
		return nil
	}

	sealed := f.batch.Seal()
	f.batch = New[T](f.cfg.BatchSize)
	f.mu.Unlock()

	select {
	case f.sealCh <- sealed:
		return nil
	case <-ctx.Done():
		f.nackAll(ctx, sealed, ctx.Err())
		return ctx.Err()
	case <-f.stopCh:
		f.nackAll(ctx, sealed, ErrFlusherClosed)
		return ErrFlusherClosed
	}
}

// Close gracefully shuts down, flushes pending batches, waits for completion.
func (f *Flusher[T]) Close(ctx context.Context) error {
	f.closeOnce.Do(func() {
		close(f.stopCh)
	})

	// Wait for flushLoop to complete
	done := make(chan struct{})
	go func() {
		f.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (f *Flusher[T]) flushLoop(ctx context.Context) {
	defer close(f.doneCh)

	ticker := time.NewTicker(f.cfg.FlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-f.stopCh:
			f.drainAndFlush(ctx)
			return
		case <-ctx.Done():
			f.drainAndFlush(ctx)
			return
		case sealed := <-f.sealCh:
			f.flush(ctx, sealed)
		case <-ticker.C:
			f.flushPartial(ctx)
		}
	}
}

func (f *Flusher[T]) drainAndFlush(ctx context.Context) {
	for {
		select {
		case sealed := <-f.sealCh:
			f.flush(ctx, sealed)
		default:
			f.flushPartial(ctx)
			return
		}
	}
}

func (f *Flusher[T]) flushPartial(ctx context.Context) {
	f.mu.Lock()
	if f.batch.Len() == 0 {
		f.mu.Unlock()
		return
	}
	sealed := f.batch.Seal()
	f.batch = New[T](f.cfg.BatchSize)
	f.mu.Unlock()

	f.flush(ctx, sealed)
}

func (f *Flusher[T]) flush(ctx context.Context, records []Record[T]) {
	if len(records) == 0 {
		return
	}

	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("batch: FlushFunc panicked", "panic", r)
				if e, ok := r.(error); ok {
					err = e
				} else {
					err = ErrFlusherClosed
				}
			}
		}()
		err = f.flushFn(ctx, records)
	}()

	if err != nil {
		f.nackAll(ctx, records, err)
		return
	}
	f.ackAll(ctx, records)
}

func (f *Flusher[T]) ackAll(ctx context.Context, records []Record[T]) {
	if f.callbacks.Ack == nil {
		return
	}
	for _, r := range records {
		f.callbacks.Ack(ctx, r.Checkpoint)
	}
}

func (f *Flusher[T]) nackAll(ctx context.Context, records []Record[T], err error) {
	if f.callbacks.Nack == nil {
		return
	}
	for _, r := range records {
		f.callbacks.Nack(ctx, r.Frame, err)
	}
}
