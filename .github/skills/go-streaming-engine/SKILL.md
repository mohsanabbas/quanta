---
name: go-streaming-engine
description: >
  Skill for designing and implementing the Quanta streaming engine core:
  pipeline lifecycle, pluggable source/sink adapters, gRPC transport layer,
  backpressure, checkpointing, and graceful shutdown. Use when creating or
  modifying engine, pipeline, transport, source, or sink packages.
argument-hint: "Describe the engine component to build or modify"
---

# Go Streaming Engine — Architecture & Implementation Skill

## Target Architecture

Quanta is a **pluggable streaming/event-processing engine**. Events flow through
a pipeline: `Source → Transform Chain → Sink(s)`, with checkpoint-based
acknowledgments flowing backward for at-least-once delivery.

### Design Principles

1. **Interface-driven pluggability** — Sources, transformers, and sinks are
   behind small interfaces defined where consumed. New implementations require
   zero changes to the engine core.
2. **Backpressure-aware** — The engine never buffers unboundedly. Every stage
   either blocks when capacity is reached or signals the upstream to pause.
3. **Checkpoint-based delivery** — The engine tracks per-event checkpoint tokens.
   Sinks acknowledge after successful publish; the source commits only when all
   downstream processing is confirmed.
4. **Graceful shutdown** — Every goroutine respects `context.Context` cancellation.
   On shutdown: stop accepting new events → drain in-flight → flush sinks →
   commit final offsets → close connections.
5. **Observable** — Telemetry via OpenTelemetry (OTel) on every boundary:
   source read, transform call, sink publish, ack, drop, error. Metrics
   integration is deferred — do not embed counters in domain structs.

---

## Package Layout (Target)

```
cmd/engine/main.go          — Entry point, driver registration, Bootstrap
internal/
  engine/
    bootstrap.go             — Wire: config → source + pipeline + transport + metrics
    config.go                — Engine-level config (ports, pipeline path)
    engine.go                — Engine lifecycle (Run, Shutdown)
  pipeline/
    compiler.go              — YAML → Runner (source + stages + sinks)
    runner.go                — Processing loop: source.Run → pushFrame → sinks
    builtins_sinks.go        — Register built-in sink drivers
  transform/
    client.go                — Client interface (consumed by pipeline)
    grpc_client.go           — gRPC implementation of Client
    inproc_client.go         — In-process implementation of Client
  transport/
    server.go                — gRPC server hosting Control + Health services
    client.go                — Engine-side gRPC client utilities
  config/
    pipeline.go              — PipelineSpec YAML schema
    kafka.go                 — Kafka config schema
  errors/
    errors.go                — Domain-typed errors (Kind/Component/Op), AsType extraction, wrapping
  telemetry/
    metrics.go               — OTel metric definitions (deferred)
  logging/
    logging.go               — Structured logging setup
source/
  source.go                  — Source Adapter interface + registry
  kafka/                     — Kafka source implementation
sink/
  registry.go                — Sink Adapter interface + registry
  kafka/                     — Kafka sink driver
  stdout/                    — Stdout sink driver
api/proto/v1/                — Protobuf definitions
pkg/checkpoint/              — Checkpoint utilities
```

---

## Core Interfaces

### Source Adapter

```go
// source/source.go — define interface here, implement in source/kafka/
type Adapter interface {
    // Configure initializes the source from parsed config.
    Configure(ctx context.Context, cfg any) error

    // Run starts consuming and calls emit for each Frame.
    // Blocks until ctx is canceled or a fatal error occurs.
    Run(ctx context.Context, emit func(context.Context, *pb.Frame) error) error

    // Close releases resources.
    Close(ctx context.Context) error
}

type Registration struct {
    Name        string
    New         func() Adapter
    ConfigProto func() any
}
```

### Transform Client

```go
// internal/transform/client.go — consumed by pipeline
type Client interface {
    Transform(ctx context.Context, req *pb.TransformRequest) (*pb.TransformResponse, error)
    Stream(ctx context.Context, opts ...grpc.CallOption) (pb.TransformService_TransformStreamClient, error)
    Metadata(ctx context.Context) (*pb.MetadataResponse, error)
    Health(ctx context.Context) (*pb.HealthResponse, error)
    Close() error
}
```

### Sink Adapter

```go
// sink/registry.go — consumed by pipeline
type Adapter interface {
    Configure(ctx context.Context, cfg any) error
    Publish(ctx context.Context, frame *pb.Frame) error
    Close(ctx context.Context) error
}

type AckAware interface {
    BindAck(fn func(*pb.CheckpointToken))
}
```

---

## Plugin Registration Pattern

All pluggable components use the same registration idiom:

```go
// In the driver package's register.go
func init() {
    sink.Register(sink.Registration{
        Name:        "kafka",
        New:         func() sink.Adapter { return &Driver{} },
        ConfigProto: func() any { return &Config{} },
    })
}

// In the registry package
var _registry = map[string]Registration{}

func Register(r Registration) {
    if r.Name == "" {
        panic("registration missing name")
    }
    _registry[r.Name] = r
}

func Lookup(name string) (Registration, bool) {
    reg, ok := _registry[name]
    return reg, ok
}
```

- `init()` panics are acceptable for invalid registrations (compile-time safety).
- Constructors return concrete types; registry factories may return interfaces.
- Sinks use `ConfigProto()` for YAML unmarshaling; sources use `LoadConfig()` for file-based config.
- Registry `New()` returns only `(Adapter, error)` — no config proto bundled in the return.

---

## Pipeline Runner Design

The Runner is the core processing loop:

```go
type Runner struct {
    source Source
    stages []transformStage
    sinks  []sink.Adapter

    mu   sync.Mutex
    subs []func(*pb.ConnectorAck)
}
```

Metrics/telemetry will be integrated later via OpenTelemetry — do not embed
counters directly into domain structs.

### Processing Flow

1. `source.Run(ctx, emit)` — source calls `emit` with each Frame.
2. `pushFrame(ctx, frame)` — frames pass through transform stages sequentially.
3. Each stage: `toRequest(frame)` → `client.Transform(ctx, req)` → `toFrames(resp)`.
4. After all stages: publish to every sink.
5. On success: `Ack(frame.Checkpoint)` — notify source to advance commit offset.
6. On drop/error: log via `logging.Warnf`, still ack to avoid stalling.

### Error Handling in Pipeline

- Errors use domain constructors at origination: `qerr.Config("kafka", "validate", err)`, `qerr.Source(...)`, `qerr.Sink(...)`, etc.
- Errors are wrapped via `qerr.Wrap` / `qerr.Wrapf` when bubbling up across boundaries.
- Use `qerr.Extract(err)` or `qerr.IsConfig(err)` for programmatic inspection (Go 1.26 `errors.AsType`).
- Leaf errors created via stdlib `errors.New` / `fmt.Errorf`.
- Transform errors: retry with backoff up to configured attempts, then drop+ack.
- Sink publish errors: return error to source → source handles redelivery.
- Use `pb.Status` enum: OK, DROP, RETRY, ERROR — each has distinct behavior.

---

## Backpressure

Three strategies, composable:

| Strategy | Mechanism |
|----------|-----------|
| Count-based | Semaphore limiting concurrent in-flight messages |
| Size-based | Total byte budget across all in-flight messages |
| Combined | Both count + size limits (default) |

```go
type BackpressureManager interface {
    Acquire(ctx context.Context, size int64) error
    Release(size int64)
    Capacity() int64
}
```

- `Acquire` blocks when capacity exhausted — backpressure propagates to source.
- `Release` called from ack path after sink confirms.

---

## Checkpointing

Sliding-window checkpoint manager with bitfield tracking:

```go
type CheckpointManager interface {
    Track(offset int64, size int64) error
    Ack(offset int64) (AckHandle, int64, bool)
    Base() int64
    Reset() []AckHandle
    Close()
}
```

- Offsets tracked in a window relative to `base`.
- Out-of-order acks supported — `base` advances only when all preceding offsets acked.
- Commit strategies: ack-driven, periodic, hybrid.

---

## Graceful Shutdown Sequence

```
1. ctx.Cancel()  →  source stops reading
2. source.Run() returns  →  in-flight frames still processing
3. drain: wait for all pushFrame goroutines to complete
4. runner.Close():
   a. close all transform clients
   b. close all sinks (flush buffers)
5. source commits final offsets
6. transport.Stop()  →  gRPC server shuts down
7. Engine.Run() returns
```

---

## Configuration Schema (YAML)

```yaml
source:
  kind: kafka          # registry lookup key
  driver: sarama       # sub-driver (sarama, kgo, confluent)
  config_file: kafka_source.yml

transformers:
  - name: uppercase
    type: grpc          # "grpc" or "inproc"
    address: localhost:8081
    timeout: 5s
    retry:
      attempts: 3
      backoff: 100ms

sinks:
  - name: stdout
  - name: kafka
    config:
      brokers: [localhost:9092]
      topic: output-topic
```

---

## Testing Patterns

Follow **TDD Red-Green-Refactor** for all engine work:
1. **Red** — Write a failing test defining desired behavior.
2. **Green** — Minimal code to pass.
3. **Refactor** — Clean up, tests stay green.

### Libraries

| Library | Purpose |
|---------|--------|
| `go.uber.org/mock/gomock` | Mock `transform.Client`, `sink.Adapter`, `source.Adapter` |
| `github.com/stretchr/testify/assert` | Fluent non-fatal assertions |
| `github.com/stretchr/testify/require` | Fatal assertions for preconditions |
| `go.uber.org/goleak` | Detect goroutine leaks — critical for engine code |

### Unit Tests
- Table-driven with `tests` slice, `tt` variable, `give`/`want` naming.
- Generate mocks with `mockgen` for boundary interfaces:
  ```bash
  mockgen -source=internal/transform/client.go -destination=internal/transform/mock_client_test.go -package=transform
  ```
- Test the Runner with mock source, mock transform client, mock sink.
- Every test that spawns goroutines must use `defer goleak.VerifyNone(t)`.

```go
func TestRunnerPushFrame(t *testing.T) {
    defer goleak.VerifyNone(t)
    ctrl := gomock.NewController(t)

    mockSink := NewMockAdapter(ctrl)
    mockSink.EXPECT().
        Publish(gomock.Any(), gomock.Any()).
        Return(nil).
        Times(1)

    r := NewRunner()
    r.AddSink(mockSink)

    frame := &pb.Frame{Key: []byte("k"), Value: []byte("v")}
    err := r.pushFrame(context.Background(), frame)
    require.NoError(t, err)
}
```

### Integration Tests
- Use `testcontainers-go` for Kafka.
- Verify end-to-end: produce → transform → consume from sink topic.
- Assert checkpoint offsets committed correctly.
- Use `require` for setup, `assert` for verification.

### Benchmarks
- Benchmark `pushFrame` with varying stage counts.
- Benchmark checkpoint manager Ack throughput.

### Goroutine Leak Detection

```go
// Add to every package with goroutines
func TestMain(m *testing.M) {
    goleak.VerifyTestMain(m)
}
```

---

## Common Mistakes to Avoid

1. **Goroutine leaks** — Every `go func()` must have a shutdown path via ctx or done channel.
2. **Ack before publish** — Never ack a checkpoint before all sinks confirm.
3. **Blocking in emit** — Source emit callbacks must not block indefinitely; backpressure should be bounded.
4. **Hardcoded source type** — Runner should accept `source.Adapter`, not `kafka.Adapter`.
5. **Giant interfaces** — Break into small interfaces (`Publisher`, `Closer`, `HealthChecker`).
6. **Panics in hot path** — Transform/sink errors must be returned, never panic.
