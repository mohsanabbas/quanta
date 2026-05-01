# REASONS Canvas: sink/batch Package

**Date**: 2025-05-01  
**Component**: `sink/batch` — Reusable batching infrastructure for streaming sinks  
**Analysis Source**: `.spdd/analysis-olap-sink.md`

---

## R — Requirements

### Problem Statement
Quanta's batching sinks (S3, future ClickHouse/DuckDB) each implement their own batching logic. This leads to:
- Code duplication across sinks
- Inconsistent delivery guarantees
- Higher risk of subtle ack/nack bugs
- Harder to test batching edge cases in isolation

### Acceptance Criteria

1. **Generic Batch Accumulator**: Type-parameterized `Batch[T]` that works with any record type
2. **Flush Lifecycle Manager**: `Flusher[T]` that handles accumulate → seal → flush → ack/nack
3. **Delivery Guarantees**:
   - Every record added MUST eventually be ack'd or nack'd (no silent drops)
   - Batch flush is atomic: all succeed or all fail
   - Graceful shutdown flushes partial batch before returning
4. **Thread Safety**: Safe for concurrent `Publish()` calls from pipeline
5. **Configurable Triggers**: Flush on batch size OR time interval (whichever comes first)
6. **Testable**: No global state, injectable dependencies, comprehensive unit tests

### Definition of Done
- [ ] `Batch[T]` with Append, Seal, Reset, Len, ByteSize
- [ ] `Flusher[T]` with Start, Add, Close
- [ ] `Pool[T]` for batch reuse (optional optimization)
- [ ] Unit tests covering all edge cases in Safeguards section
- [ ] Zero goleak goroutine leaks
- [ ] Integrated into at least one sink (ClickHouse or refactored S3)

---

## E — Entities

### Core Types

```go
// Record wraps sink-specific data with checkpoint metadata.
// T is the sink-specific record type ([]byte for S3, []any for ClickHouse).
type Record[T any] struct {
    Data       T                      // Sink-specific payload
    Checkpoint *pb.CheckpointToken    // For ack on success
    Frame      *pb.Frame              // For nack on failure (needed for DLQ)
}

// Batch accumulates records until sealed.
type Batch[T any] struct {
    records  []Record[T]
    byteSize int64
    capacity int
    mu       sync.Mutex
}

// FlusherConfig controls batching behavior.
type FlusherConfig struct {
    BatchSize     int
    FlushInterval time.Duration
}

// FlushFunc is the sink-specific flush implementation.
// Contract: process all records atomically, return nil on success.
type FlushFunc[T any] func(ctx context.Context, records []Record[T]) error

// Callbacks wraps ack/nack functions from sink.BuildOptions.
type Callbacks struct {
    Ack  sink.EmitFn   // func(ctx, *CheckpointToken)
    Nack sink.NackFn   // func(ctx, *Frame, error)
}

// Flusher manages the batch lifecycle.
type Flusher[T any] struct {
    batch     *Batch[T]
    flushFn   FlushFunc[T]
    callbacks Callbacks
    cfg       FlusherConfig
    
    mu        sync.Mutex
    sealCh    chan []Record[T]
    stopCh    chan struct{}
    doneCh    chan struct{}
    stopOnce  sync.Once
    closeOnce sync.Once
    sealWg    sync.WaitGroup
}

// Pool manages reusable Batch instances.
type Pool[T any] struct {
    pool     *sync.Pool
    capacity int
}
```

### Relationships

```
┌─────────────────────────────────────────────────────────────────┐
│                         Flusher[T]                              │
│  ┌─────────────┐     ┌─────────────┐     ┌─────────────────┐   │
│  │  Batch[T]   │────▶│   sealCh    │────▶│   flushLoop()   │   │
│  │  (current)  │     │  (buffer 1) │     │   goroutine     │   │
│  └─────────────┘     └─────────────┘     └────────┬────────┘   │
│        ▲                                          │             │
│        │ Add()                                    │ flush()     │
│        │                                          ▼             │
│  ┌─────┴─────┐                            ┌───────────────┐    │
│  │ Publish() │                            │  FlushFunc[T] │    │
│  │  caller   │                            │  (sink impl)  │    │
│  └───────────┘                            └───────┬───────┘    │
│                                                   │             │
│                                    success: ackAll() ──▶ Ack    │
│                                    failure: nackAll() ─▶ Nack   │
└─────────────────────────────────────────────────────────────────┘
```

### Dependency on Existing Types

```go
import (
    pb "quanta/api/proto/v1"  // CheckpointToken, Frame
    "quanta/sink"              // EmitFn, NackFn
)
```

---

## A — Approach

### Strategy: Extract & Generalize

1. **Extract** batching logic from `sink/s3/batch.go` and `sink/s3/driver.go`
2. **Generalize** with Go generics (`Batch[T]`, `Flusher[T]`)
3. **Decouple** flush logic via `FlushFunc[T]` callback
4. **Prove** with unit tests before integrating into sinks

### Concurrency Decision (Rule 0)

**Question**: Should we support concurrent flushes?

**Analysis**:
1. Current S3 sink uses single flushLoop — works fine
2. ClickHouse batch INSERT is I/O-bound but single connection saturates ~100k rows/sec
3. Concurrent flushes add complexity: ordering, error handling, backpressure

**Decision**: **Single sequential flushLoop** for v1.
- Simpler to reason about
- Easier to test
- Add `MaxConcurrentFlushes` config later if proven necessary

### Key Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Batch mutex | Yes, for Append | Concurrent Publish() calls need protection |
| Seal mutex | Held during swap | Prevents Append during batch swap |
| sealCh buffer | 1 | Avoids blocking Add when batch fills |
| sealWg | Track in-flight seals | Ensures graceful shutdown waits for sends |
| Batch reuse | Optional Pool[T] | Reduces allocations for high-throughput |
| FlushFunc contract | All-or-nothing | Matches Quanta's atomic batch semantics |

### Patterns Used

1. **Generic Type Parameter**: `Batch[T]`, `Flusher[T]`, `Pool[T]`
2. **Callback Injection**: `FlushFunc[T]` for sink-specific logic
3. **Background Goroutine**: `flushLoop` for time-based flushes
4. **Graceful Shutdown**: `sealWg.Wait()` → `close(stopCh)` → drain → flush partial
5. **sync.Once**: Idempotent Close

---

## S — Structure

### Package Layout

```
sink/
  batch/
    batch.go          # Batch[T] type
    batch_test.go     # Unit tests for Batch
    flusher.go        # Flusher[T] type + flushLoop
    flusher_test.go   # Unit tests for Flusher (most complex)
    pool.go           # Pool[T] for batch reuse
    pool_test.go      # Unit tests for Pool
    errors.go         # ErrFlusherClosed
    doc.go            # Package documentation
```

### File Responsibilities

| File | LOC (est) | Responsibility |
|------|-----------|----------------|
| `batch.go` | ~80 | Batch accumulator: New, Append, Seal, Reset, Len, ByteSize |
| `flusher.go` | ~180 | Lifecycle manager: NewFlusher, Start, Add, Close, flushLoop |
| `pool.go` | ~30 | sync.Pool wrapper for batch reuse |
| `errors.go` | ~10 | Sentinel error: ErrFlusherClosed |
| `doc.go` | ~20 | Package-level documentation |
| `*_test.go` | ~400 | Comprehensive tests |

### Dependencies

```
sink/batch
  ├── quanta/api/proto/v1  (pb.CheckpointToken, pb.Frame)
  ├── quanta/sink          (EmitFn, NackFn types)
  ├── sync                 (Mutex, Once, WaitGroup, Pool)
  ├── time                 (Ticker, Duration)
  ├── context              (Context, Done)
  └── errors               (New)
```

No external dependencies. Pure Go stdlib + internal types.

---

## O — Operations

### Operation 1: Batch[T] Implementation

**File**: `sink/batch/batch.go`

```go
// Package batch provides reusable batching infrastructure for streaming sinks.
//
// The batch package separates concerns:
//   - Batch[T]: accumulates records of any type
//   - Flusher[T]: manages lifecycle (seal on size/time, flush, ack/nack)
//   - Pool[T]: optional batch reuse for reduced allocations
//
// Delivery guarantees:
//   - Every Add'd record is eventually ack'd or nack'd (no silent drops)
//   - Flush is atomic: all records succeed or all fail
//   - Graceful shutdown flushes partial batch before Close returns
package batch

import (
	"sync"

	pb "quanta/api/proto/v1"
)

// Record wraps sink-specific data with checkpoint metadata needed for ack/nack.
type Record[T any] struct {
	// Data is the sink-specific payload (e.g., []byte for S3, []any for ClickHouse).
	Data T

	// Checkpoint is passed to Ack callback on successful flush.
	Checkpoint *pb.CheckpointToken

	// Frame is passed to Nack callback on failed flush (needed for DLQ).
	Frame *pb.Frame
}

// Batch accumulates records until sealed. Thread-safe for concurrent Append calls.
type Batch[T any] struct {
	records  []Record[T]
	byteSize int64
	capacity int
	mu       sync.Mutex
}

// New creates a Batch with the given capacity.
// Capacity determines when Append returns true (batch full).
func New[T any](capacity int) *Batch[T] {
	if capacity <= 0 {
		capacity = 100 // sensible default
	}
	return &Batch[T]{
		records:  make([]Record[T], 0, capacity),
		capacity: capacity,
	}
}

// Append adds a record to the batch. Returns true if batch is now full.
// byteSize is an optional hint for backpressure tracking (0 if not used).
// Thread-safe: may be called concurrently from multiple goroutines.
func (b *Batch[T]) Append(data T, cp *pb.CheckpointToken, f *pb.Frame, byteSize int64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.records = append(b.records, Record[T]{Data: data, Checkpoint: cp, Frame: f})
	b.byteSize += byteSize
	return len(b.records) >= b.capacity
}

// Len returns the current number of records in the batch.
func (b *Batch[T]) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.records)
}

// ByteSize returns the accumulated byte size of all records.
func (b *Batch[T]) ByteSize() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.byteSize
}

// Seal extracts all records and resets the batch for reuse.
// Returns nil if batch is empty.
// Thread-safe but should not race with Append (Flusher coordinates this).
func (b *Batch[T]) Seal() []Record[T] {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.records) == 0 {
		return nil
	}
	out := b.records
	b.records = make([]Record[T], 0, b.capacity)
	b.byteSize = 0
	return out
}

// Reset clears the batch without returning records.
// Used when batch contents should be discarded (e.g., abort scenario).
func (b *Batch[T]) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.records = b.records[:0]
	b.byteSize = 0
}
```

---

### Operation 2: Errors

**File**: `sink/batch/errors.go`

```go
package batch

import "errors"

// ErrFlusherClosed is returned by Add when the Flusher has been closed.
var ErrFlusherClosed = errors.New("batch: flusher closed")
```

---

### Operation 3: Flusher[T] Implementation

**File**: `sink/batch/flusher.go`

```go
package batch

import (
	"context"
	"sync"
	"time"

	pb "quanta/api/proto/v1"
	"quanta/sink"
)

// FlushFunc is called with sealed records to persist them.
//
// Contract:
//   - MUST process all records or none (atomic batch semantics)
//   - Return nil on success (all records persisted)
//   - Return error on failure (none persisted; Flusher will nack all)
//
// Implementations: S3 upload, ClickHouse batch INSERT, DuckDB appender, etc.
type FlushFunc[T any] func(ctx context.Context, records []Record[T]) error

// Callbacks wraps ack/nack functions for delivery confirmation.
type Callbacks struct {
	Ack  sink.EmitFn // Called per-record on successful flush
	Nack sink.NackFn // Called per-record on failed flush
}

// FlusherConfig controls batching behavior.
type FlusherConfig struct {
	// BatchSize triggers flush when this many records accumulate.
	BatchSize int

	// FlushInterval triggers flush after this duration, even if batch is not full.
	FlushInterval time.Duration
}

// Flusher manages the batch lifecycle: accumulate → seal → flush → ack/nack.
//
// Concurrency model:
//   - Add() may be called concurrently from multiple Publish() goroutines
//   - flushLoop() runs in a dedicated goroutine (started by Start)
//   - Only one flush executes at a time (sequential flushes)
//   - Close() is graceful: waits for in-flight work, flushes partial batch
type Flusher[T any] struct {
	batch     *Batch[T]
	flushFn   FlushFunc[T]
	callbacks Callbacks
	cfg       FlusherConfig

	mu        sync.Mutex       // Protects batch swap
	sealCh    chan []Record[T] // Sealed batches waiting for flush
	stopCh    chan struct{}    // Signals flushLoop to stop
	doneCh    chan struct{}    // Closed when flushLoop exits
	stopOnce  sync.Once
	closeOnce sync.Once
	sealWg    sync.WaitGroup // Tracks in-flight seal sends
}

// NewFlusher creates a Flusher with the given configuration.
// Call Start() to begin the flush loop, then Add() to enqueue records.
func NewFlusher[T any](cfg FlusherConfig, flushFn FlushFunc[T], cb Callbacks) *Flusher[T] {
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 100
	}
	if cfg.FlushInterval <= 0 {
		cfg.FlushInterval = 5 * time.Second
	}
	return &Flusher[T]{
		batch:     New[T](cfg.BatchSize),
		flushFn:   flushFn,
		callbacks: cb,
		cfg:       cfg,
		sealCh:    make(chan []Record[T], 1), // Buffer 1 to avoid blocking Add
		stopCh:    make(chan struct{}),
		doneCh:    make(chan struct{}),
	}
}

// Start begins the flush loop goroutine. Call once after NewFlusher.
// The flush loop runs until Close() is called or ctx is cancelled.
func (f *Flusher[T]) Start(ctx context.Context) {
	go f.flushLoop(ctx)
}

// Add appends a record to the current batch.
//
// If the batch becomes full, it is sealed and sent to the flush loop.
// Thread-safe: may be called concurrently from multiple goroutines.
//
// Returns ErrFlusherClosed if the flusher has been closed.
// Returns ctx.Err() if the context is cancelled before the sealed batch
// could be sent to the flush loop.
func (f *Flusher[T]) Add(ctx context.Context, data T, cp *pb.CheckpointToken, frame *pb.Frame, byteSize int64) error {
	f.mu.Lock()

	// Check if closed
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

	// Batch is full — seal and prepare to send
	sealed := f.batch.Seal()
	f.batch = New[T](f.cfg.BatchSize)
	f.sealWg.Add(1)
	f.mu.Unlock()

	// Send outside lock to avoid blocking other Add() calls
	defer f.sealWg.Done()
	select {
	case f.sealCh <- sealed:
		return nil
	case <-ctx.Done():
		// Context cancelled before send — nack all records
		f.nackAll(ctx, sealed, ctx.Err())
		return ctx.Err()
	case <-f.stopCh:
		// Flusher closed before send — nack all records
		f.nackAll(ctx, sealed, ErrFlusherClosed)
		return ErrFlusherClosed
	}
}

// Close gracefully shuts down the flusher.
//
// Shutdown sequence:
//  1. Wait for any in-flight Add() calls to finish sending to sealCh
//  2. Signal stop to flushLoop
//  3. flushLoop drains sealCh and flushes any partial batch
//  4. flushLoop exits, closing doneCh
//
// Returns nil on success, ctx.Err() if context expires before shutdown completes.
func (f *Flusher[T]) Close(ctx context.Context) error {
	f.closeOnce.Do(func() {
		// Wait for in-flight Add() calls that are sending to sealCh
		f.sealWg.Wait()

		// Signal flushLoop to stop
		f.stopOnce.Do(func() { close(f.stopCh) })
	})

	// Wait for flushLoop to finish
	select {
	case <-f.doneCh:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// flushLoop runs in a dedicated goroutine, processing sealed batches
// and triggering time-based flushes.
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

// drainAndFlush processes any remaining sealed batches and the partial batch.
// Called during shutdown.
func (f *Flusher[T]) drainAndFlush(ctx context.Context) {
	// Drain sealed batches from channel
	for {
		select {
		case sealed := <-f.sealCh:
			f.flush(ctx, sealed)
		default:
			// Channel empty — flush partial batch
			f.flushPartial(ctx)
			return
		}
	}
}

// flushPartial seals and flushes the current batch if non-empty.
// Called on timer tick and during shutdown.
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

// flush calls the FlushFunc and acks or nacks all records based on result.
func (f *Flusher[T]) flush(ctx context.Context, records []Record[T]) {
	if len(records) == 0 {
		return
	}

	err := f.flushFn(ctx, records)
	if err != nil {
		f.nackAll(ctx, records, err)
		return
	}
	f.ackAll(ctx, records)
}

// ackAll calls the Ack callback for each record's checkpoint.
func (f *Flusher[T]) ackAll(ctx context.Context, records []Record[T]) {
	if f.callbacks.Ack == nil {
		return
	}
	for _, r := range records {
		f.callbacks.Ack(ctx, r.Checkpoint)
	}
}

// nackAll calls the Nack callback for each record's frame.
func (f *Flusher[T]) nackAll(ctx context.Context, records []Record[T], err error) {
	if f.callbacks.Nack == nil {
		return
	}
	for _, r := range records {
		f.callbacks.Nack(ctx, r.Frame, err)
	}
}
```

---

### Operation 4: Pool[T] Implementation

**File**: `sink/batch/pool.go`

```go
package batch

import "sync"

// Pool manages reusable Batch instances to reduce allocations.
// Optional optimization for high-throughput sinks.
type Pool[T any] struct {
	pool     *sync.Pool
	capacity int
}

// NewPool creates a Pool that produces batches with the given capacity.
func NewPool[T any](capacity int) *Pool[T] {
	if capacity <= 0 {
		capacity = 100
	}
	return &Pool[T]{
		capacity: capacity,
		pool: &sync.Pool{
			New: func() any { return New[T](capacity) },
		},
	}
}

// Get retrieves a batch from the pool (or creates a new one).
func (p *Pool[T]) Get() *Batch[T] {
	return p.pool.Get().(*Batch[T])
}

// Put returns a batch to the pool after resetting it.
func (p *Pool[T]) Put(b *Batch[T]) {
	b.Reset()
	p.pool.Put(b)
}
```

---

### Operation 5: Package Documentation

**File**: `sink/batch/doc.go`

```go
// Package batch provides reusable batching infrastructure for Quanta streaming sinks.
//
// # Overview
//
// The batch package separates batching concerns into composable components:
//
//   - [Batch]: Accumulates records of any type until sealed
//   - [Flusher]: Manages the lifecycle (seal on size/time, flush, ack/nack)
//   - [Pool]: Optional batch reuse for reduced allocations
//
// # Delivery Guarantees
//
// The package enforces Quanta's at-least-once delivery semantics:
//
//   - Every record added via [Flusher.Add] is eventually ack'd or nack'd
//   - Flush is atomic: all records in a batch succeed or fail together
//   - On flush failure, all records are nack'd (triggering redelivery or DLQ)
//   - Graceful shutdown flushes any partial batch before [Flusher.Close] returns
//
// # Usage
//
// Sinks use the Flusher to manage batching. The sink provides a [FlushFunc]
// that persists records to the target system (S3, ClickHouse, etc.):
//
//	flushFn := func(ctx context.Context, records []batch.Record[[]byte]) error {
//	    // Encode and upload to S3
//	    return s3Client.PutObject(ctx, encode(records))
//	}
//
//	flusher := batch.NewFlusher(batch.FlusherConfig{
//	    BatchSize:     1000,
//	    FlushInterval: 5 * time.Second,
//	}, flushFn, batch.Callbacks{Ack: opts.Ack, Nack: opts.Nack})
//
//	flusher.Start(ctx)
//	defer flusher.Close(ctx)
//
//	// In Publish():
//	return flusher.Add(ctx, frame.Value, frame.Checkpoint, frame, int64(len(frame.Value)))
//
// # Thread Safety
//
// [Flusher.Add] is safe for concurrent calls from multiple goroutines.
// [Flusher.Start] and [Flusher.Close] should be called once each.
package batch
```

---

## N — Norms

### Discovered Conventions (from existing codebase)

| Convention | Example | Applied Here |
|------------|---------|--------------|
| Package comment in doc.go | `sink/s3`, `source/kafka` | `sink/batch/doc.go` |
| Struct fields lowercase | `type batch struct { records... }` | `type Batch[T] struct { records... }` |
| Private defaults with underscore | `_defaultBatchSize` | Applied in FlusherConfig validation |
| sync.Once for idempotent ops | `closeOnce sync.Once` | Used in Flusher.Close |
| Channels for goroutine coordination | `stopCh`, `doneCh` | Used in Flusher |
| Test file naming | `*_test.go` | `batch_test.go`, `flusher_test.go` |
| testify for assertions | `require.NoError`, `assert.Equal` | Will use in tests |
| goleak for goroutine checks | `go.uber.org/goleak` | Will use in tests |

### Error Handling

| Error | When | Action |
|-------|------|--------|
| `ErrFlusherClosed` | Add after Close | Returned to caller |
| `ctx.Err()` | Context cancelled | Nack pending, return error |
| FlushFunc error | Flush fails | Nack all records in batch |

### Logging

- No logging in batch package (let sinks log)
- Errors propagate via nack callbacks
- Flusher is transparent — sinks log their own errors

---

## S — Safeguards

### Invariants

1. **No Silent Drops**: Every `Add()`'d record MUST eventually trigger `Ack` or `Nack`
2. **Atomic Batch**: FlushFunc receives all records; on error, ALL are nack'd
3. **Graceful Shutdown**: `Close()` waits for `sealWg`, drains `sealCh`, flushes partial
4. **No Goroutine Leaks**: `flushLoop` exits on `stopCh` or `ctx.Done()`
5. **Thread Safety**: Concurrent `Add()` calls protected by mutex

### Edge Cases & Mitigations

| Edge Case | Mitigation | Test |
|-----------|------------|------|
| Add after Close | Return `ErrFlusherClosed` | `TestAdd_AfterClose` |
| Context cancelled during Add | Nack sealed batch, return `ctx.Err()` | `TestAdd_ContextCancelled` |
| FlushFunc panics | Recover in flush(), nack all, log | `TestFlush_Panic` |
| Double Close | `closeOnce` prevents double shutdown | `TestClose_Idempotent` |
| Empty batch at shutdown | `flushPartial` checks Len() | `TestClose_EmptyBatch` |
| Concurrent Add fills batch | Mutex protects swap | `TestAdd_Concurrent` |
| sealCh full (buffer=1) | Second sealer blocks until first drains | `TestAdd_BackPressure` |
| Nil callbacks | Check `if cb.Ack == nil` before call | `TestCallbacks_Nil` |

### Race Conditions Prevented

| Scenario | Prevention |
|----------|------------|
| Concurrent Append | `Batch.mu` mutex |
| Append during Seal | `Flusher.mu` holds during swap |
| Add during Close | `stopCh` checked under `Flusher.mu` |
| Double sealWg.Done | `defer` ensures exactly once |

### Test Plan

```go
// batch_test.go
func TestBatch_New(t *testing.T)
func TestBatch_Append_ReturnsFull(t *testing.T)
func TestBatch_Append_Concurrent(t *testing.T)
func TestBatch_Seal_ReturnsRecords(t *testing.T)
func TestBatch_Seal_Empty(t *testing.T)
func TestBatch_Reset(t *testing.T)
func TestBatch_ByteSize(t *testing.T)

// flusher_test.go
func TestFlusher_Add_Success(t *testing.T)
func TestFlusher_Add_BatchFull_Flushes(t *testing.T)
func TestFlusher_Add_AfterClose(t *testing.T)
func TestFlusher_Add_ContextCancelled(t *testing.T)
func TestFlusher_Add_Concurrent(t *testing.T)
func TestFlusher_FlushInterval_Triggers(t *testing.T)
func TestFlusher_FlushFunc_Error_NacksAll(t *testing.T)
func TestFlusher_FlushFunc_Success_AcksAll(t *testing.T)
func TestFlusher_Close_FlushesPartial(t *testing.T)
func TestFlusher_Close_Idempotent(t *testing.T)
func TestFlusher_Close_DrainsSealed(t *testing.T)
func TestFlusher_NoGoroutineLeak(t *testing.T)  // goleak
func TestFlusher_Callbacks_Nil(t *testing.T)

// pool_test.go
func TestPool_GetPut(t *testing.T)
func TestPool_Concurrent(t *testing.T)
```

---

## Summary

| Section | Key Points |
|---------|------------|
| **R** | Generic batching, delivery guarantees, flush on size/time |
| **E** | `Batch[T]`, `Flusher[T]`, `Record[T]`, `FlushFunc[T]`, `Callbacks` |
| **A** | Sequential flushLoop, mutex coordination, graceful shutdown |
| **S** | 5 files, ~300 LOC impl + ~400 LOC tests, pure stdlib |
| **O** | 5 operations with complete code |
| **N** | Follows existing sink patterns, testify, goleak |
| **S** | 5 invariants, 8 edge cases, comprehensive test plan |

---

## Next Steps

1. `spdd-go-generate` → Create implementation files
2. `spdd-go-test` → Generate test files
3. Integrate into ClickHouse sink (or refactor S3 sink)
