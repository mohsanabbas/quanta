---
name: go-style
description: >
  Go coding style conventions for the Quanta project, synthesized from
  Effective Go, Uber Go Style Guide, Google Go Style Guide, and CloudQuery
  plugin architecture patterns. Use when writing or reviewing any Go code.
argument-hint: "Describe what Go code to write or review"
---

# Go Style — Quanta Project Conventions

## Sources

These conventions are synthesized from:
- [Effective Go](https://go.dev/doc/effective_go)
- [Uber Go Style Guide](https://github.com/uber-go/guide/blob/master/style.md)
- [Google Go Style Guide](https://google.github.io/styleguide/go/)
- [CloudQuery Plugin SDK](https://github.com/cloudquery/plugin-sdk) architecture patterns

---

## Package Design

### Naming
- Lowercase, single-word, no underscores: `pipeline`, `transform`, `sink`.
- Package name is the base name of its directory.
- Don't repeat the package name in exported symbols: `sink.Adapter` not `sink.SinkAdapter`.
- Avoid generic names: no `util`, `common`, `shared`, `lib`, `helpers`.

### Layout
- `cmd/` — entry points only. Thin `main()` that calls `run()` returning error.
- `internal/` — private packages. Core engine logic.
- `pkg/` — public utilities intended for external consumption.
- `api/` — protocol definitions (proto files + generated code).
- Top-level packages (`source/`, `sink/`) — pluggable adapter interfaces + registry + drivers.

### internal vs pkg
Use `internal/` for engine internals that should never be imported by plugins.
Use `pkg/` for utilities plugins may need (e.g., checkpoint helpers, plugin SDK).

---

## Error Handling

The `internal/errors` package (`qerr`) provides domain-typed errors with
Go 1.26 `errors.AsType` support, plus wrapping utilities.

### The `internal/errors` Package

Domain constructors for error origination + Wrap/Wrapf for bubbling up:

```go
import qerr "quanta/internal/errors"
```

**Domain Kinds:** `KindConfig`, `KindSource`, `KindSink`, `KindTransform`, `KindTransport`, `KindPipeline`.

**Structured Error:** `qerr.Error{Kind, Component, Op, Err}` — implements `error` and `Unwrap()`.

### Domain Constructors (at origination)
```go
// Good — domain constructor where error is first observed
return qerr.Config("kafka", "validate", errors.New("brokers required"))
return qerr.Source("kafka", "configure", err)
return qerr.Sink("stdout", "publish", err)
return qerr.Transform("uppercase", "dial", err)
return qerr.Transport("grpc", "listen", err)
return qerr.Pipeline("compile", err)
```

### Wrapping (bubbling up)
```go
// Good — Wrap when adding context at package boundaries
return qerr.Wrap(err, "pipeline compile")
return qerr.Wrapf(err, "sink %s publish", name)

// Bad — raw fmt.Errorf with %w
return fmt.Errorf("pipeline compile: %w", err)
```

### Type-Safe Extraction (Go 1.26 errors.AsType)
```go
// Extract the domain error from anywhere in the chain
if e, ok := qerr.Extract(err); ok {
    log.Printf("kind=%s component=%s op=%s", e.Kind, e.Component, e.Op)
}

// Convenience kind checkers
if qerr.IsConfig(err) { /* validation failure */ }
if qerr.IsSource(err) { /* source adapter issue */ }
```

### Creating Leaf Errors
```go
// Use stdlib directly — qerr never creates leaf errors
return errors.New("source not configured")
return fmt.Errorf("unsupported driver %q", name)
```

### Sentinel Errors
```go
// Defined where consumed, using stdlib
var ErrCheckpointClosed = errors.New("kafka: checkpoint manager closed")
```

### Handle Once
Either wrap+return OR log+degrade. Never both.

```go
// Good — domain constructor and return
if err := doThing(); err != nil {
    return qerr.Source("kafka", "do-thing", err)
}

// Good — log and degrade (via logger)
if err := emitMetric(); err != nil {
    logging.Warnf("emit metric: %s", err)
    // continue — metrics are not critical
}

// Bad — log AND return
if err := doThing(); err != nil {
    log.Printf("do thing failed: %v", err)
    return err  // caller will also log
}
```

---

## Interfaces

### Define Where Consumed
```go
// Good — pipeline package defines the interface it needs
// internal/pipeline/runner.go
type transformClient interface {
    Transform(ctx context.Context, req *pb.TransformRequest) (*pb.TransformResponse, error)
    Close() error
}

// Bad — transform package exports interface for its own types
// internal/transform/interfaces.go  ← don't do this
```

Exception: when an interface IS the package's primary export (e.g., `sink.Adapter`).

### Size
- 1-2 methods: ideal. Name with `-er` suffix.
- 3-5 methods: acceptable for primary contracts like `Client`.
- 6+: split into composed interfaces.

### Compile-Time Verification
```go
var _ Client = (*GRPCClient)(nil)
var _ Client = (*InProcessClient)(nil)
var _ sink.Adapter = (*Driver)(nil)
```

---

## Concurrency

### Goroutine Lifecycle
Every goroutine must have a shutdown path:

```go
// Good — context-based shutdown
func (w *Worker) Run(ctx context.Context) error {
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        case msg := <-w.inbox:
            w.process(msg)
        }
    }
}

// Good — done channel
func (w *Worker) Run() {
    defer close(w.done)
    for {
        select {
        case <-w.stop:
            return
        case msg := <-w.inbox:
            w.process(msg)
        }
    }
}
```

### Mutex
```go
type Cache struct {
    mu   sync.RWMutex  // named mu, never embedded
    data map[string]string
}
```

### Channel Sizing
```go
ch := make(chan *Frame)      // unbuffered — synchronization point
ch := make(chan *Frame, 1)   // buffered — decoupling
// Larger sizes need a comment justifying the choice:
ch := make(chan *Frame, 100) // 100: max in-flight per partition
```

---

## Constructors

```go
// Return concrete type from constructor
func NewRunner() *Runner {
    return &Runner{
        stages: make([]transformStage, 0, 4),
    }
}

// Accept interface in function params
func Compile(ctx context.Context, src source.Adapter, ...) (*Runner, error)
```

**Exception — registry factories:** Functions like `source.New(name)` or `sink.New(name)`
that dispatch to different concrete types at runtime may return interfaces. This is the
standard Go pattern (cf. `sql.Open`, `image.Decode`). Concrete `NewXxx()` constructors
must still return the concrete type.

---

## Testing

### TDD: Red-Green-Refactor

All new features and bug fixes follow TDD:

1. **Red** — Write a failing test that defines the desired behavior.
2. **Green** — Write the minimum code to make the test pass.
3. **Refactor** — Clean up the implementation while keeping tests green.

```
# TDD workflow for a new feature:
1. Write test → go test → FAIL (red)
2. Implement minimal code → go test → PASS (green)
3. Refactor → go test → PASS (still green)
4. Repeat for next behavior
```

Commit after each green step. Tests are first-class code — treat them with
the same quality standards as production code.

### Test Libraries

```go
import (
    "testing"

    "github.com/stretchr/testify/assert"   // fluent assertions
    "github.com/stretchr/testify/require"  // assertions that stop the test
    "go.uber.org/mock/gomock"              // interface mocking
    "go.uber.org/goleak"                   // goroutine leak detection
)
```

| Library | Use For |
|---------|--------|
| `go.uber.org/mock/gomock` | Generate and use interface mocks |
| `github.com/stretchr/testify/assert` | Non-fatal assertions |
| `github.com/stretchr/testify/require` | Fatal assertions (stop test on failure) |
| `go.uber.org/goleak` | Detect goroutine leaks in tests |

### Table-Driven Tests

```go
func TestCheckpointAck(t *testing.T) {
    tests := []struct {
        name     string
        give     int64   // offset to ack
        wantBase int64   // expected new base
        wantOK   bool    // whether base advanced
    }{
        {name: "first offset",  give: 0, wantBase: 1, wantOK: true},
        {name: "out of order",  give: 2, wantBase: 1, wantOK: false},
        {name: "fill gap",      give: 1, wantBase: 3, wantOK: true},
    }

    cm := NewCheckpointManager(64)
    for i := int64(0); i < 3; i++ {
        cm.Track(i, 100)
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            _, base, ok := cm.Ack(tt.give)
            assert.Equal(t, tt.wantBase, base, "base offset")
            assert.Equal(t, tt.wantOK, ok, "base advanced")
        })
    }
}
```

### Mocking with gomock

Generate mocks from interfaces:

```bash
# Install mockgen
go install go.uber.org/mock/mockgen@latest

# Generate mock for an interface
mockgen -source=internal/transform/client.go -destination=internal/transform/mock_client_test.go -package=transform

# Or use go:generate directive in the source file
//go:generate mockgen -source=client.go -destination=mock_client_test.go -package=transform
```

Using mocks in tests:

```go
func TestRunnerTransformStage(t *testing.T) {
    ctrl := gomock.NewController(t)

    mockClient := NewMockClient(ctrl)
    mockClient.EXPECT().
        Transform(gomock.Any(), gomock.Any()).
        Return(&pb.TransformResponse{
            Status: pb.Status_OK,
            Events: []*pb.Event{{Value: []byte("transformed")}},
        }, nil).
        Times(1)

    r := NewRunner()
    r.AddTransformer("test", mockClient, 5*time.Second, 3, 100*time.Millisecond)
    // ... exercise and assert
}
```

### Assertions with testify

```go
// require — stops test immediately on failure (use for preconditions)
result, err := doThing()
require.NoError(t, err)
require.NotNil(t, result)

// assert — continues test on failure (use for verifications)
assert.Equal(t, "expected", result.Name)
assert.Len(t, result.Items, 3)
assert.Contains(t, result.Tags, "important")
assert.ErrorIs(t, err, ErrNotFound)
assert.ErrorAs(t, err, &target)
```

### Goroutine Leak Detection

```go
// Per-test leak check
func TestWorkerShutdown(t *testing.T) {
    defer goleak.VerifyNone(t)

    w := NewWorker()
    ctx, cancel := context.WithCancel(context.Background())
    go w.Run(ctx)

    cancel()
    w.Wait()
}

// Package-level leak check (in TestMain)
func TestMain(m *testing.M) {
    goleak.VerifyTestMain(m)
}
```

### Naming
- `give` prefix for inputs, `want` prefix for expected outputs.
- `tests` for the slice, `tt` for the iterator variable.
- Test functions: `TestXxx` in same package (white-box) or `_test` package (black-box).
- Mock files: `mock_xxx_test.go` — keep mocks test-only.

### Testing Guidelines
- Write the test FIRST (TDD red), then implement.
- Use `require` for setup/preconditions, `assert` for verifications.
- Use `gomock` for interface boundaries, not for everything.
- Prefer real implementations with fake data over mocks when possible.
- Every test with goroutines must use `goleak.VerifyNone(t)`.
- Never use `panic` in tests — use `t.Fatal` or `require`.
- Split complex conditional table tests into separate test functions.

---

## Performance

### Pre-allocate
```go
// Good
out := make([]*pb.Frame, 0, len(events))
headers := make(map[string]string, len(f.Headers))

// Bad
var out []*pb.Frame
headers := map[string]string{}
```

### String Conversions
```go
// Good — hot path
s := strconv.FormatInt(offset, 10)

// Acceptable — cold path
s := fmt.Sprintf("%d", offset)
```

### Avoid Repeated Conversions
```go
// Bad — converts on every call
for _, msg := range messages {
    w.Write([]byte("prefix"))  // allocates each time
}

// Good — convert once
prefix := []byte("prefix")
for _, msg := range messages {
    w.Write(prefix)
}
```

---

## Registration Pattern (CloudQuery-Inspired)

All pluggable components (sources, sinks, in-process transformers) follow:

```go
// 1. Interface in consumer package
type Adapter interface { ... }

// 2. Registration struct
type Registration struct {
    Name        string
    New         func() Adapter
    ConfigProto func() any
}

// 3. Package-level registry
var _registry = map[string]Registration{}

func Register(r Registration) {
    if r.Name == "" {
        panic("registration missing name")
    }
    _registry[r.Name] = r
}

func New(name string) (Adapter, any, error) {
    reg, ok := _registry[name]
    if !ok {
        return nil, nil, fmt.Errorf("unknown adapter %q", name)
    }
    return reg.New(), reg.ConfigProto(), nil
}

// 4. Driver registers in init()
func init() {
    sink.Register(sink.Registration{
        Name: "kafka",
        New:  func() sink.Adapter { return &Driver{} },
        ConfigProto: func() any { return &Config{} },
    })
}
```

---

## Functional Options

For constructors with 3+ optional parameters:

```go
type Option func(*options)

type options struct {
    timeout  time.Duration
    attempts int
    logger   *slog.Logger
}

func WithTimeout(d time.Duration) Option {
    return func(o *options) { o.timeout = d }
}

func NewClient(target string, opts ...Option) (*Client, error) {
    o := options{
        timeout:  _defaultTimeout,
        attempts: _defaultAttempts,
    }
    for _, opt := range opts {
        opt(&o)
    }
    // ...
}
```

---

## Code Organization

```go
// File ordering within a .go file:
// 1. package declaration
// 2. imports (stdlib | third-party — separated by blank line)
// 3. constants
// 4. package-level variables (with _ prefix for unexported)
// 5. type definitions
// 6. constructors (NewXxx)
// 7. exported methods (grouped by receiver)
// 8. unexported methods
// 9. standalone helper functions

// Function ordering: rough call order (callers before callees)
```
