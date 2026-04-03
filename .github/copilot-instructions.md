# Quanta — Copilot Workspace Instructions

## Project Overview

Quanta is a modular streaming/event-processing engine written in Go. It consumes events from pluggable sources (currently Kafka), runs them through an ordered chain of transformer plugins via gRPC, and forwards transformed events to one or more pluggable sinks.

**Module path:** `quanta`
**Go version:** 1.26+
**Build:** `make build`
**Test:** `go test ./...`
**Lint:** `golangci-lint run ./...`
**Proto codegen:** `make proto`

## Architecture

```
Source → Runner (pipeline) → [Transform Stage 1] → [Transform Stage N] → Sink(s)
   ↑                                                                        |
   └─────────── ack callback (checkpoint token) ────────────────────────────┘
```

### Core Packages

| Package | Responsibility |
|---------|---------------|
| `cmd/engine/` | Entry point, driver registration, Bootstrap call |
| `internal/engine/` | Bootstrap, Engine lifecycle, Config |
| `internal/pipeline/` | Compiler (YAML→Runner), Runner (processing loop) |
| `internal/transform/` | `Client` interface, `GRPCClient`, `InProcessClient` |
| `internal/transport/` | gRPC server, Control service |
| `internal/config/` | Pipeline and Kafka config loading |
| `internal/telemetry/` | Prometheus metrics |
| `source/kafka/` | Kafka source adapter (Sarama driver, backpressure, checkpointing) |
| `sink/` | Sink registry, stdout/kafka drivers |
| `api/proto/v1/` | Protobuf definitions (Frame, Transform, Control, Health, Connector) |
| `pkg/checkpoint/` | Checkpoint utilities |

### Key Interfaces

```go
// Transform client — engine calls transformers through this interface
type Client interface {
    Metadata(ctx context.Context) (*pb.MetadataResponse, error)
    Health(ctx context.Context) (*pb.HealthResponse, error)
    Transform(ctx context.Context, req *pb.TransformRequest) (*pb.TransformResponse, error)
    Stream(ctx context.Context, opts ...grpc.CallOption) (pb.TransformService_TransformStreamClient, error)
    Close() error
}

// Sink adapter — all sinks implement this
type Adapter interface {
    Configure(ctx context.Context, cfg any) error
    Publish(ctx context.Context, frame *pb.Frame) error
    Close(ctx context.Context) error
}

// Kafka source interfaces: BackpressureManager, CheckpointManager, CommitStrategy, PartitionProcessor
```

### Plugin Registration Pattern

Sinks and source drivers use a registration pattern:
```go
func init() {
    sink.Register(sink.Registration{
        Name: "kafka",
        New:  func() sink.Adapter { return &Driver{} },
        ConfigProto: func() any { return &Config{} },
    })
}
```

## Code Style

Follow these Go conventions strictly:

### Naming
- Package names: lowercase, single-word, no underscores (`pipeline`, `transform`, `sink`)
- Interfaces: `-er` suffix for single-method (`Reader`, `Writer`); descriptive noun for multi-method (`Client`, `Adapter`, `Manager`)
- Unexported globals: prefix with `_` (`_defaultTimeout`, `_registry`)
- Error variables: `ErrXxx` (exported), `errXxx` (unexported)
- Error types: `XxxError` suffix
- Constructors: `New` or `NewXxx` — return concrete types; registry factories may return interfaces

### Error Handling
- Always handle errors — never discard with `_` except in deferred Close
- Use `internal/errors` package (`qerr`) for **domain-typed errors** and **wrapping**
- Domain constructors at origination: `qerr.Config("kafka", "validate", err)`, `qerr.Source(...)`, `qerr.Sink(...)`, `qerr.Transform(...)`, `qerr.Transport(...)`, `qerr.Pipeline("compile", err)`
- `qerr.Wrap(err, "context")` / `qerr.Wrapf(...)` for adding context when bubbling up
- Use `errors.AsType[*qerr.Error](err)` (Go 1.26) or `qerr.Extract(err)` for type-safe extraction
- Kind checkers: `qerr.IsConfig(err)`, `qerr.IsSource(err)`, etc.
- Use stdlib `errors.New` / `fmt.Errorf` for leaf error creation — `qerr` wraps, never creates leaf errors
- Never use raw `fmt.Errorf` with `%w` for wrapping — always go through `qerr`
- Keep Op strings short — no "failed to" prefix
- Use `errors.Is` for sentinel matching, `qerr.Extract` for domain inspection
- Return errors, don't panic — except `init()` for invalid registrations
- Handle each error exactly once: either wrap+return OR log+degrade via `logging.Warnf`

### Interfaces
- Define interfaces where they are consumed, not where they are implemented
- Keep interfaces small — prefer composition of small interfaces
- Verify interface compliance at compile time: `var _ Interface = (*Impl)(nil)`
- Return concrete types from constructors; accept interfaces in function params

### Concurrency
- "Do not communicate by sharing memory; share memory by communicating"
- Every goroutine must have a clear shutdown path via `context.Context` or done channel
- Use `sync.WaitGroup` for fan-out, channels for pipeline stages
- Mutex fields: named `mu`, never embedded in public structs
- No goroutines in `init()`

### Testing
- **TDD Red-Green-Refactor**: write failing test first, implement, refactor
- Table-driven tests with `tests` slice, `tt` iterator, `give`/`want` prefixes
- Use `t.Run(tt.name, ...)` for subtests
- Mock interfaces with `go.uber.org/mock/gomock` (`mockgen` for code generation)
- Assertions with `github.com/stretchr/testify` — `require` for preconditions, `assert` for verifications
- Goroutine leak detection with `go.uber.org/goleak` — `defer goleak.VerifyNone(t)`
- Test file naming: `xxx_test.go` in same package

### Structure
- Functions sorted by rough call order
- Exported functions after type definitions
- Constructor (`NewXxx`) immediately after type
- Group related declarations
- Reduce nesting — handle errors early, return/continue immediately

### Performance
- Pre-allocate slices/maps with known capacity: `make([]T, 0, n)`
- Use `strconv` over `fmt.Sprint` for conversions
- Avoid repeated `string` ↔ `[]byte` conversions on hot paths
- Buffer channels: size 0 (unbuffered) or 1; larger sizes need justification

## Proto Contract

The canonical Frame flows through the system:
```protobuf
message Frame {
    bytes key = 1;
    bytes value = 2;
    map<string, bytes> headers = 3;
    google.protobuf.Timestamp ts = 4;
    CheckpointToken checkpoint = 5;
}
```

CheckpointToken is source-agnostic (oneof: KafkaOffset, SqsHandle, HttpAckID, raw bytes).

Transform plugins implement `TransformService`:
- `Transform` — unary RPC (current)
- `TransformStream` — bidirectional streaming (target architecture)
- `Health` / `Metadata` — introspection

## Configuration

Pipeline is defined in YAML (`pipeline.yml`):
```yaml
source:
  kind: kafka
  driver: sarama
  config_file: kafka_source.yml

transformers:
  - name: uppercase
    type: grpc
    address: localhost:8081

sinks:
  - stdout
  - kafka
```

## Dependencies

- `google.golang.org/grpc` — gRPC framework
- `google.golang.org/protobuf` — Protocol Buffers
- `github.com/IBM/sarama` — Kafka client (Sarama)
- `gopkg.in/yaml.v3` — YAML parsing
- `github.com/prometheus/client_golang` — Metrics
