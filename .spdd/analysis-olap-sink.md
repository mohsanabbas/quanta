# SPDD Analysis: OLAP Sink for Quanta (Revised)

**Date**: 2025-05-01  
**Requirement**: Add an open-source OLAP database sink (ClickHouse first, DuckDB later)  
**Revision**: Added reusable batching package analysis, column mapping design, delivery semantics

---

## 1. Requirement Analysis

### User Story
As a Quanta user, I want to stream data into an OLAP database for analytical queries, so that I can run fast aggregations on ingested streaming data.

### Scope Expansion (Per Discussion)
1. **Reusable Batching Package**: Extract common batching logic from S3 sink
2. **Config-Driven Column Mapping**: Unified approach for ClickHouse, DuckDB, S3 Parquet
3. **Delivery Semantics**: At-least-once with proper ack/nack handling

---

## 2. Delivery Semantics Analysis

### Current Architecture (Source → Pipeline → Sink)

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                            QUANTA DELIVERY MODEL                            │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  Source (Kafka)              Pipeline                    Sink              │
│  ┌─────────────┐         ┌─────────────┐          ┌─────────────┐          │
│  │PartitionProc│         │AckCoordinator│         │ClickHouse   │          │
│  │             │         │             │          │             │          │
│  │ Track(off)──┼────────▶│Barrier(tok,n)│         │             │          │
│  │             │         │             │          │             │          │
│  │             │  frame  │             │  frame   │             │          │
│  │             │────────▶│─────────────┼─────────▶│ Publish()   │          │
│  │             │         │             │          │             │          │
│  │             │         │             │◀─────────│ack(tok)     │          │
│  │             │         │             │   OR     │nack(f,err)  │          │
│  │             │         │             │          │             │          │
│  │◀────────────┼─────────│commit(tok)  │          │             │          │
│  │MarkOffset   │         │             │          │             │          │
│  └─────────────┘         └─────────────┘          └─────────────┘          │
│                                                                             │
│  INVARIANTS:                                                                │
│  1. Every frame MUST eventually ack OR nack (no silent drops)              │
│  2. Nack without DLQ → withhold commit → source redelivers                 │
│  3. Nack with DLQ → publish to DLQ → commit (poison pill removed)          │
│  4. Batch sink: ack/nack ALL frames in batch atomically                    │
│  5. Shutdown: flush pending batch, then ack/nack, then close               │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Key Observations from Codebase

1. **AckCoordinator Barrier Pattern**: When frame fans out to N sinks, `Barrier(tok, N)` is created. Each sink calls `Ack()` → refcount decrements. When refs=0 → commit to source.

2. **Nack Semantics**:
   - With DLQ: Publish to DLQ, then commit (remove poison pill)
   - Without DLQ: Withhold commit → message will be redelivered
   - Both: Abort the barrier immediately

3. **S3 Sink Current Batching**: 
   - Accumulates frames in `batch` struct
   - Seals batch on size OR flush interval
   - Uploads to S3
   - Acks ALL checkpoints on success
   - Nacks ALL frames on failure

4. **Kafka Source Checkpoint**:
   - `PartitionTracker` uses bitmap for out-of-order acks
   - `CommitStrategy` decides when to actually commit
   - Supports hybrid (count + time) commit policy

---

## 3. Reusable Batching Package Design

### Why Current S3 Batch Is Not Reusable

```go
// s3/batch.go - tightly coupled
type batch struct {
    records     [][]byte           // S3-specific: raw bytes
    checkpoints []*pb.CheckpointToken
    frames      []*pb.Frame
    count       int
    size        int                // byte size tracking
    capacity    int
}
```

Problems:
1. `records [][]byte` is S3-specific (encodes to JSONL)
2. No abstraction over "what is a record"
3. Flush logic is intertwined with S3 upload in driver.go
4. No separation of concerns (batching vs flushing vs encoding)

### Proposed Design: `sink/batch` Package

#### Core Principles

1. **Single Responsibility**: Batch accumulates, Flusher manages lifecycle
2. **Open/Closed**: Extensible via interfaces, closed for modification
3. **Liskov Substitution**: Any FlushFunc works with any Batch
4. **Interface Segregation**: Small, focused interfaces
5. **Dependency Inversion**: Depend on abstractions, not concrete sinks

#### Design

```go
// sink/batch/batch.go
package batch

import (
    "sync"
    pb "quanta/api/proto/v1"
)

// Record represents one item in a batch. Sinks provide their own record type.
// The batch package doesn't care what T is—it just accumulates and hands off.
type Record[T any] struct {
    Data       T
    Checkpoint *pb.CheckpointToken
    Frame      *pb.Frame  // Needed for nack (original frame for DLQ)
}

// Batch accumulates records until sealed. Thread-safe for Append, but
// Seal must not race with Append (caller coordinates via Flusher).
type Batch[T any] struct {
    records  []Record[T]
    byteSize int64
    capacity int
    mu       sync.Mutex  // Protects append during concurrent Publish calls
}

func New[T any](capacity int) *Batch[T] {
    return &Batch[T]{
        records:  make([]Record[T], 0, capacity),
        capacity: capacity,
    }
}

// Append adds a record. Returns true if batch is now full.
// byteSize is optional hint for backpressure (0 if not tracked).
func (b *Batch[T]) Append(data T, cp *pb.CheckpointToken, f *pb.Frame, byteSize int64) bool {
    b.mu.Lock()
    defer b.mu.Unlock()
    b.records = append(b.records, Record[T]{Data: data, Checkpoint: cp, Frame: f})
    b.byteSize += byteSize
    return len(b.records) >= b.capacity
}

// Len returns current record count.
func (b *Batch[T]) Len() int {
    b.mu.Lock()
    defer b.mu.Unlock()
    return len(b.records)
}

// ByteSize returns accumulated byte size.
func (b *Batch[T]) ByteSize() int64 {
    b.mu.Lock()
    defer b.mu.Unlock()
    return b.byteSize
}

// Seal extracts all records and resets the batch for reuse.
// Caller must ensure no concurrent Append during Seal.
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

// Reset clears without returning records (e.g., on abort).
func (b *Batch[T]) Reset() {
    b.mu.Lock()
    defer b.mu.Unlock()
    b.records = b.records[:0]
    b.byteSize = 0
}
```

```go
// sink/batch/flusher.go
package batch

import (
    "context"
    "sync"
    "time"
    
    pb "quanta/api/proto/v1"
    "quanta/sink"
)

// FlushFunc is called with sealed records. Implementations:
// - S3: encode to JSONL/Parquet, upload to S3
// - ClickHouse: batch insert
// - DuckDB: appender insert
//
// Contract:
// - MUST process all records or none (atomic)
// - Returns nil on success (all records persisted)
// - Returns error on failure (none persisted, caller will nack)
type FlushFunc[T any] func(ctx context.Context, records []Record[T]) error

// Callbacks matches sink.BuildOptions for ack/nack.
type Callbacks struct {
    Ack  sink.EmitFn
    Nack sink.NackFn
}

// FlusherConfig controls batching behavior.
type FlusherConfig struct {
    BatchSize     int
    FlushInterval time.Duration
    // Future: MaxBatchBytes, MaxConcurrentFlushes
}

// Flusher manages the batch lifecycle: accumulate → seal → flush → ack/nack.
// 
// Concurrency model:
// - Add() is called from Publish (may be concurrent from multiple goroutines)
// - flushLoop() runs in dedicated goroutine
// - Seal happens in flushLoop OR in Add when batch is full
// - Only one flush at a time (sequential flushes)
type Flusher[T any] struct {
    batch     *Batch[T]
    flushFn   FlushFunc[T]
    callbacks Callbacks
    cfg       FlusherConfig
    
    mu         sync.Mutex    // Protects batch swap
    sealCh     chan []Record[T]
    stopCh     chan struct{}
    doneCh     chan struct{}
    stopOnce   sync.Once
    closeOnce  sync.Once
    
    // For graceful shutdown: wait for in-flight seals to be sent to sealCh
    sealWg     sync.WaitGroup
}

func NewFlusher[T any](cfg FlusherConfig, flushFn FlushFunc[T], cb Callbacks) *Flusher[T] {
    f := &Flusher[T]{
        batch:     New[T](cfg.BatchSize),
        flushFn:   flushFn,
        callbacks: cb,
        cfg:       cfg,
        sealCh:    make(chan []Record[T], 1),  // Buffer 1 to avoid blocking Add
        stopCh:    make(chan struct{}),
        doneCh:    make(chan struct{}),
    }
    return f
}

// Start begins the flush loop. Call once.
func (f *Flusher[T]) Start(ctx context.Context) {
    go f.flushLoop(ctx)
}

// Add appends a record to the current batch. If batch becomes full,
// seals and sends to flush loop. Thread-safe.
//
// Returns error only if flusher is closed or context cancelled.
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
    
    // Batch is full—seal and send
    sealed := f.batch.Seal()
    f.batch = New[T](f.cfg.BatchSize)  // Fresh batch for next records
    f.sealWg.Add(1)
    f.mu.Unlock()
    
    // Send outside lock to avoid blocking
    defer f.sealWg.Done()
    select {
    case f.sealCh <- sealed:
        return nil
    case <-ctx.Done():
        // Context cancelled before we could send—nack all
        f.nackAll(ctx, sealed, ctx.Err())
        return ctx.Err()
    case <-f.stopCh:
        // Flusher closed—nack all
        f.nackAll(ctx, sealed, ErrFlusherClosed)
        return ErrFlusherClosed
    }
}

// Close gracefully shuts down: waits for in-flight seals, signals stop,
// flushLoop will drain sealCh and flush partial batch, then exit.
func (f *Flusher[T]) Close(ctx context.Context) error {
    f.closeOnce.Do(func() {
        // Wait for any in-flight Add() calls to finish sending to sealCh
        f.sealWg.Wait()
        
        // Signal stop
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
    // Drain any sealed batches waiting in channel
    for {
        select {
        case sealed := <-f.sealCh:
            f.flush(ctx, sealed)
        default:
            goto flushPartial
        }
    }
flushPartial:
    // Flush any partial batch
    f.flushPartial(ctx)
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
    
    err := f.flushFn(ctx, records)
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

var ErrFlusherClosed = errors.New("flusher closed")
```

```go
// sink/batch/pool.go
package batch

import "sync"

// Pool manages reusable Batch instances to reduce allocations.
type Pool[T any] struct {
    pool     *sync.Pool
    capacity int
}

func NewPool[T any](capacity int) *Pool[T] {
    return &Pool[T]{
        capacity: capacity,
        pool: &sync.Pool{
            New: func() any { return New[T](capacity) },
        },
    }
}

func (p *Pool[T]) Get() *Batch[T] {
    return p.pool.Get().(*Batch[T])
}

func (p *Pool[T]) Put(b *Batch[T]) {
    b.Reset()
    p.pool.Put(b)
}
```

### Edge Cases Handled

| Scenario | Handling |
|----------|----------|
| **Batch full during Add** | Seal immediately, send to flushLoop, new batch created |
| **Flush interval fires** | Seal partial batch if non-empty |
| **Flush fails** | Nack ALL records in batch → AckCoordinator handles DLQ/redelivery |
| **Context cancelled during Add** | Nack sealed batch, return error |
| **Graceful shutdown** | sealWg.Wait() → stop → drain sealCh → flush partial |
| **Concurrent Publish calls** | mu protects batch swap; Batch.Append is mutex-protected |
| **Double Close** | closeOnce prevents double shutdown |

### Delivery Guarantees

1. **At-Least-Once**: If flush fails, nack triggers redelivery (or DLQ)
2. **Atomic Batch**: All records in batch succeed or fail together
3. **No Silent Drops**: Every Add'd record is eventually ack'd or nack'd
4. **Ordered Acks**: Acks happen after successful flush, in batch order
5. **Graceful Shutdown**: Partial batch is flushed before Close returns

---

## 4. Config-Driven Column Mapping

### Use Cases

| Sink | Input | Output |
|------|-------|--------|
| S3 Parquet | `pb.Frame` | Parquet row with typed columns |
| ClickHouse | `pb.Frame` | INSERT row with typed columns |
| DuckDB | `pb.Frame` | Appender row with typed columns |

### Unified Column Mapping Config

```yaml
# Common schema across sinks
column_mapping:
  columns:
    - name: "event_time"
      type: "timestamp"     # Logical type (mapped to sink-specific)
      source: "ts"          # Frame field: ts, key, value, header:<name>
      nullable: false
      
    - name: "user_id"
      type: "string"
      source: "header:x-user-id"
      nullable: true
      default: ""
      
    - name: "event_key"
      type: "bytes"
      source: "key"
      
    - name: "payload"
      type: "json"          # Parsed from value
      source: "value"
      
    - name: "raw_data"
      type: "bytes"
      source: "value"       # Same source, different type = no parse
```

### Package Structure

```go
// sink/schema/mapping.go
package schema

import pb "quanta/api/proto/v1"

// LogicalType is sink-agnostic. Each sink maps to native type.
type LogicalType string

const (
    TypeString    LogicalType = "string"
    TypeBytes     LogicalType = "bytes"
    TypeInt64     LogicalType = "int64"
    TypeFloat64   LogicalType = "float64"
    TypeBool      LogicalType = "bool"
    TypeTimestamp LogicalType = "timestamp"
    TypeJSON      LogicalType = "json"
)

// Source identifies where to extract data from a Frame.
type Source struct {
    Kind      SourceKind  // TS, Key, Value, Header
    HeaderKey string      // If Kind == Header
}

type SourceKind int

const (
    SourceTS SourceKind = iota
    SourceKey
    SourceValue
    SourceHeader
)

// Column defines one output column.
type Column struct {
    Name     string
    Type     LogicalType
    Source   Source
    Nullable bool
    Default  any  // Used if source is nil and nullable
}

// Mapping is the full schema definition.
type Mapping struct {
    Columns []Column
}

// Row extracts values from a Frame according to the mapping.
// Returns []any in column order, suitable for batch insert.
func (m *Mapping) Extract(f *pb.Frame) ([]any, error) {
    row := make([]any, len(m.Columns))
    for i, col := range m.Columns {
        val, err := extractValue(f, col)
        if err != nil {
            return nil, fmt.Errorf("column %s: %w", col.Name, err)
        }
        row[i] = val
    }
    return row, nil
}

func extractValue(f *pb.Frame, col Column) (any, error) {
    var raw any
    switch col.Source.Kind {
    case SourceTS:
        if f.Ts != nil {
            raw = f.Ts.AsTime()
        }
    case SourceKey:
        raw = f.Key
    case SourceValue:
        raw = f.Value
    case SourceHeader:
        if f.Headers != nil {
            raw = f.Headers[col.Source.HeaderKey]
        }
    }
    
    if raw == nil {
        if col.Nullable {
            return col.Default, nil
        }
        return nil, errors.New("required field is nil")
    }
    
    return coerce(raw, col.Type)
}

func coerce(val any, typ LogicalType) (any, error) {
    // Type coercion logic...
}
```

### Sink-Specific Type Mapping

```go
// sink/clickhouse/types.go
var clickhouseTypes = map[schema.LogicalType]string{
    schema.TypeString:    "String",
    schema.TypeBytes:     "String",  // or LowCardinality(String)
    schema.TypeInt64:     "Int64",
    schema.TypeFloat64:   "Float64",
    schema.TypeBool:      "Bool",
    schema.TypeTimestamp: "DateTime64(3)",
    schema.TypeJSON:      "JSON",  // ClickHouse 23.1+
}

// sink/duckdb/types.go
var duckdbTypes = map[schema.LogicalType]string{
    schema.TypeString:    "VARCHAR",
    schema.TypeBytes:     "BLOB",
    schema.TypeInt64:     "BIGINT",
    schema.TypeFloat64:   "DOUBLE",
    schema.TypeBool:      "BOOLEAN",
    schema.TypeTimestamp: "TIMESTAMP",
    schema.TypeJSON:      "JSON",
}

// sink/s3/parquet/types.go
// Uses Arrow schema for Parquet
```

---

## 5. Updated File Structure

```
sink/
  batch/
    batch.go          # Generic Batch[T]
    batch_test.go
    flusher.go        # Flusher[T] with flush loop
    flusher_test.go
    pool.go           # sync.Pool wrapper
    pool_test.go
    errors.go         # ErrFlusherClosed
    
  schema/
    mapping.go        # Column, Mapping, Extract
    mapping_test.go
    types.go          # LogicalType enum
    parse.go          # YAML config parser
    
  clickhouse/
    config.go         # Config with column_mapping
    driver.go         # Uses batch.Flusher[[]any]
    driver_test.go
    types.go          # ClickHouse type mapping
    register.go
    
  duckdb/            # Future
    config.go
    driver.go
    types.go
    register.go
    
  s3/
    # Refactor to use batch.Flusher[[]byte]
    # Add parquet encoder with schema.Mapping
    
  kafka/
    # No change (async producer handles batching)
    
  stdout/
    # No change (debug sink)
```

---

## 6. Recommendation Summary

| Priority | Item | Rationale |
|----------|------|-----------|
| **1** | `sink/batch` package | Foundation for all batching sinks; fixes S3 tech debt |
| **2** | `sink/schema` package | Unified column mapping for ClickHouse, DuckDB, Parquet |
| **3** | ClickHouse sink | Production OLAP use case; validates batch + schema |
| **4** | Refactor S3 sink | Migrate to batch.Flusher; add Parquet encoder |
| **5** | DuckDB sink | Dev/edge use case; reuses batch + schema |

---

## 7. DuckDB Use Cases (Detailed)

### 7.1 Local Development

```yaml
# dev-pipeline.yaml
sinks:
  - local_analytics
sink_configs:
  local_analytics:
    driver: duckdb
    path: "./dev_events.duckdb"
    column_mapping:
      columns:
        - { name: ts, type: timestamp, source: ts }
        - { name: key, type: string, source: key }
        - { name: value, type: json, source: value }
```

Developer workflow:
```bash
# Run pipeline locally
quanta run dev-pipeline.yaml

# Query in another terminal
duckdb dev_events.duckdb "SELECT * FROM events WHERE ts > now() - INTERVAL 5 MINUTE"
```

### 7.2 Edge Analytics

```
┌─────────────────────────────────────────────┐
│              EDGE DEVICE                    │
│  ┌─────────┐   ┌─────────┐   ┌──────────┐  │
│  │ Sensor  │──▶│  Quanta │──▶│  DuckDB  │  │
│  │ Source  │   │ Pipeline│   │  (local) │  │
│  └─────────┘   └─────────┘   └──────────┘  │
│                                   │         │
│                              Periodic       │
│                              Sync           │
│                                   ▼         │
└───────────────────────────────────┼─────────┘
                                    │
                              ┌─────▼─────┐
                              │  Cloud    │
                              │ ClickHouse│
                              └───────────┘
```

Benefits:
- Process locally, no network dependency
- Aggregate at edge, ship summaries
- Works offline with local persistence

### 7.3 Smart Parquet Export

DuckDB can write Parquet to S3 directly:

```yaml
sink_configs:
  parquet_export:
    driver: duckdb
    mode: export
    output: "s3://bucket/events/year={year}/month={month}/*.parquet"
    partition_by: [year, month]
    column_mapping:
      columns:
        - { name: year, type: int64, source: "extract(ts, 'year')" }
        - { name: month, type: int64, source: "extract(ts, 'month')" }
        - { name: ts, type: timestamp, source: ts }
        - { name: data, type: json, source: value }
```

### 7.4 CI/CD Test Fixtures

```go
func TestPipeline_ErrorHandling(t *testing.T) {
    // In-memory DuckDB for fast tests
    sink, _ := duckdb.New(":memory:", schema, opts)
    
    pipeline := buildTestPipeline(sink)
    pipeline.Run(ctx, testEvents)
    
    // Assert with SQL
    var errCount int
    sink.QueryRow("SELECT COUNT(*) FROM events WHERE status = 'error'").Scan(&errCount)
    assert.Equal(t, 3, errCount)
}
```

### 7.5 Ad-Hoc Debugging Sidecar

```yaml
# Temporarily add DuckDB sink for debugging
sinks:
  - production_kafka
  - debug_duckdb  # Remove after debugging

sink_configs:
  debug_duckdb:
    driver: duckdb
    path: "/tmp/debug_stream.duckdb"
    sample_rate: 0.01  # 1% sample
```

---

## 8. Next Steps

1. **Review this analysis** — confirm batch package design
2. **Generate Canvas** for `sink/batch` package first
3. **Implement batch package** with comprehensive tests
4. **Generate Canvas** for ClickHouse sink (uses batch)
5. **Implement ClickHouse sink**
6. **Refactor S3 sink** to use batch package
7. **Add schema/mapping** for Parquet and ClickHouse
8. **Future**: DuckDB sink

Ready to proceed with Canvas generation for `sink/batch`?
