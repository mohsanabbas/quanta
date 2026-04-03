---
name: transformer-plugin
description: >
  Skill for designing and implementing Quanta transformer plugins: gRPC
  streaming protocol, in-process plugins, plugin SDK patterns, the
  TransformService proto contract, credit-based flow control, and plugin
  lifecycle management. Use when creating or modifying transform packages,
  plugin implementations, or the gRPC streaming protocol.
argument-hint: "Describe the transformer plugin feature to build"
---

# Transformer Plugin — Design & Implementation Skill

## Overview

Transformer plugins are the core extensibility mechanism of Quanta. Each
transformer receives events (Frames), applies business logic, and returns zero
or more output events. Plugins communicate with the engine via gRPC or run
in-process for zero-overhead transformations.

---

## Proto Contract

### TransformService

```protobuf
service TransformService {
  // Unary RPC — simple request/response per event.
  rpc Transform(TransformRequest) returns (TransformResponse);

  // Bidirectional streaming — high-throughput, credit-based flow control.
  rpc TransformStream(stream TransformStreamMessage) returns (stream TransformStreamMessage);

  // Introspection
  rpc Health(HealthRequest) returns (HealthResponse);
  rpc Metadata(MetadataRequest) returns (MetadataResponse);
}
```

### Message Types

```protobuf
message TransformRequest {
  string pipeline_id = 1;
  string plugin_id   = 2;
  bytes  payload     = 3;
  EventMetadata metadata = 4;
  bool   batch_mode  = 5;
}

message TransformResponse {
  repeated Event events = 1;
  Status status         = 2;
  string error_message  = 3;
  int32  retry_after_ms = 4;
}

message Event {
  string id        = 1;
  bytes  value     = 2;
  EventMetadata metadata = 3;
}

message EventMetadata {
  int64 timestamp_ms = 1;
  map<string, string> headers = 2;
  string source_partition = 3;
  string source_offset    = 4;
  map<string, string> attributes = 5;
}
```

### Status Codes

| Status | Meaning | Engine Behavior |
|--------|---------|-----------------|
| `OK` | Transform succeeded | Forward output events to next stage/sinks |
| `DROP` | Intentionally discard | Ack checkpoint, log drop |
| `RETRY` | Transient failure | Retry with backoff up to max attempts, then drop |
| `ERROR` | Permanent failure | Retry up to max attempts, then drop |

---

## Client Interface

The engine consumes transformers through this interface:

```go
// internal/transform/client.go
type Client interface {
    Transform(ctx context.Context, req *pb.TransformRequest) (*pb.TransformResponse, error)
    Stream(ctx context.Context, opts ...grpc.CallOption) (pb.TransformService_TransformStreamClient, error)
    Metadata(ctx context.Context) (*pb.MetadataResponse, error)
    Health(ctx context.Context) (*pb.HealthResponse, error)
    Close() error
}

// Compile-time interface check
var _ Client = (*GRPCClient)(nil)
var _ Client = (*InProcessClient)(nil)
```

---

## gRPC Client (Out-of-Process)

For plugins running as separate processes:

```go
type GRPCClient struct {
    conn *grpc.ClientConn
    svc  pb.TransformServiceClient
}

func NewGRPCClient(ctx context.Context, target string, opts ...grpc.DialOption) (*GRPCClient, error) {
    if len(opts) == 0 {
        opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
    }
    conn, err := grpc.NewClient(target, opts...)
    if err != nil {
        return nil, qerr.Transform("grpc", "dial", err)
    }
    return &GRPCClient{
        conn: conn,
        svc:  pb.NewTransformServiceClient(conn),
    }, nil
}
```

- Wait for connection readiness with `conn.WaitForStateChange`.
- Always close connection in `Close()`.
- Use `context.WithTimeout` per-call for unary RPCs.

---

## In-Process Client

For built-in or embedded transformers (zero network overhead):

```go
// Transformer is the interface a plugin author implements.
type Transformer interface {
    Metadata(context.Context) (*pb.MetadataResponse, error)
    Health(context.Context) (*pb.HealthResponse, error)
    Transform(context.Context, *pb.TransformRequest) (*pb.TransformResponse, error)
}

type InProcessClient struct {
    impl Transformer
}

func NewInProcessClient(impl Transformer) *InProcessClient {
    return &InProcessClient{impl: impl}
}
```

- `Stream()` returns `ErrStreamNotSupported` — streaming only via gRPC.
- `Close()` is a no-op for in-process plugins.

---

## Bidirectional Streaming Protocol (Target Design)

### Credit-Based Flow Control

```
Engine                                Plugin
  |                                      |
  |--- ControlMessage{START} ----------->|
  |<-- ControlMessage{GRANT, credits=N} -|
  |                                      |
  |--- TransformRequest[1] ------------>|
  |--- TransformRequest[2] ------------>|
  |    ...up to N requests...            |
  |                                      |
  |<-- TransformResponse[1] ------------|
  |<-- ControlMessage{GRANT, credits=1}-|  // replenish
  |                                      |
  |--- TransformRequest[N+1] ---------->|
  |    ...                               |
  |                                      |
  |--- ControlMessage{FLUSH} ---------->|
  |<-- TransformResponse[remaining] ----|
  |<-- ControlMessage{PONG} ------------|
  |                                      |
  |--- ControlMessage{STOP} ----------->|
```

### StreamMessage Wrapper

```protobuf
message TransformStreamMessage {
  oneof msg {
    TransformRequest  request  = 1;
    TransformResponse response = 2;
    ControlMessage    control  = 3;
  }
}

message ControlMessage {
  enum Type {
    START  = 0;
    STOP   = 1;
    PING   = 2;
    PONG   = 3;
    FLUSH  = 4;
    GRANT  = 5;
    PAUSE  = 6;
    RESUME = 7;
  }
  Type  type    = 1;
  int32 credits = 2;  // only used for GRANT
}
```

### Engine-Side Stream Manager

```go
type StreamManager struct {
    stream pb.TransformService_TransformStreamClient
    credits atomic.Int32
    inflight sync.WaitGroup

    sendCh chan *pb.TransformRequest     // bounded channel
    recvCh chan *pb.TransformResponse    // response dispatch

    mu      sync.Mutex
    pending map[string]*pendingRequest   // correlation by request ID
}
```

Key behaviors:
- Engine sends requests only when `credits > 0`.
- Plugin sends `GRANT` messages to replenish credits.
- `FLUSH` triggers the plugin to drain all buffered responses.
- `PAUSE`/`RESUME` for backpressure propagation.
- Correlation: match responses to requests via `pipeline_id + plugin_id + metadata`.

---

## Plugin SDK Pattern (CloudQuery-Inspired)

For plugin authors, provide an SDK that abstracts gRPC:

```go
// pkg/pluginsdk/plugin.go
type Plugin struct {
    name     string
    version  string
    handler  Handler
}

type Handler interface {
    // Transform processes a single event and returns output events.
    Transform(ctx context.Context, payload []byte, metadata map[string]string) ([]Event, error)
}

type Event struct {
    Value    []byte
    Metadata map[string]string
}

// Serve starts the gRPC server for this plugin.
func (p *Plugin) Serve(ctx context.Context, addr string) error {
    // 1. Create gRPC server
    // 2. Register TransformService with handler adapter
    // 3. Register Health + Metadata services
    // 4. Listen and serve, block until ctx canceled
}
```

### Example Plugin Implementation

```go
package main

import (
    "bytes"
    "context"
    sdk "quanta/pkg/pluginsdk"
)

type uppercaseHandler struct{}

func (h *uppercaseHandler) Transform(ctx context.Context, payload []byte, md map[string]string) ([]sdk.Event, error) {
    return []sdk.Event{
        {Value: bytes.ToUpper(payload), Metadata: md},
    }, nil
}

func main() {
    p := sdk.NewPlugin("uppercase", "1.0.0", &uppercaseHandler{})
    p.Serve(context.Background(), ":8081")
}
```

---

## Pipeline Integration

### Adding a Transform Stage

In `pipeline/compiler.go`:

```go
for _, t := range cfg.Transformers {
    var cli transform.Client
    switch t.Type {
    case "grpc":
        cli, err = transform.NewGRPCClient(ctx, t.Address)
    case "inproc":
        impl, ok := transform.LookupInProc(t.Name)
        if !ok {
            return fmt.Errorf("unknown in-proc transformer %q", t.Name)
        }
        cli = transform.NewInProcessClient(impl)
    default:
        return fmt.Errorf("unsupported transformer type %q", t.Type)
    }
    if err != nil {
        return qerr.Transform(t.Name, "create", err)
    }
    r.AddTransformer(t.Name, cli, t.Timeout(), t.Retry.Attempts, t.RetryBackoff())
}
```

### Frame ↔ Request Conversion

```go
func toRequest(f *pb.Frame) *pb.TransformRequest {
    md := &pb.EventMetadata{
        TimestampMs: f.Ts.AsTime().UnixMilli(),
    }
    // Copy headers, extract source partition/offset from checkpoint
    return &pb.TransformRequest{
        Payload:  f.Value,
        Metadata: md,
    }
}

func toFrames(orig *pb.Frame, events []*pb.Event) []*pb.Frame {
    out := make([]*pb.Frame, 0, len(events))
    for _, ev := range events {
        // Construct new Frame preserving checkpoint from original
        out = append(out, &pb.Frame{
            Key:        orig.Key,
            Value:      ev.Value,
            Ts:         orig.Ts,
            Checkpoint: orig.Checkpoint,
        })
    }
    return out
}
```

---

## Retry and Timeout

```go
var (
    _defaultTimeout  = 5 * time.Second
    _defaultAttempts = 3
    _defaultBackoff  = 100 * time.Millisecond
)
```

Retry loop in `pushFrame`:
1. Create call context with timeout.
2. Call `client.Transform(ctx, req)`.
3. On error or RETRY/ERROR status: sleep backoff, retry.
4. After max attempts: drop event, ack checkpoint, log via `logging.Warnf`.
5. On OK: forward events. On DROP: ack, log drop.

---

## Health and Metadata

```go
// Health check — used by engine for liveness probing
func (c *GRPCClient) Health(ctx context.Context) (*pb.HealthResponse, error) {
    return c.svc.Health(ctx, &pb.HealthRequest{})
}

// Metadata — plugin self-description (name, version, capabilities)
func (c *GRPCClient) Metadata(ctx context.Context) (*pb.MetadataResponse, error) {
    return c.svc.Metadata(ctx, &pb.MetadataRequest{})
}
```

Use metadata for:
- Plugin capability discovery (supports streaming? batch mode?).
- Pipeline validation at compile time.
- Dashboard/observability plugin inventory.

---

## Testing Transformers

Follow **TDD Red-Green-Refactor**: write the test first, then implement.

### Libraries

```go
import (
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
    "go.uber.org/mock/gomock"
    "go.uber.org/goleak"
)
```

### Unit Test a Plugin Handler (TDD)

```go
// RED: write this test first, before implementing uppercaseHandler
func TestUppercaseTransform(t *testing.T) {
    tests := []struct {
        name string
        give []byte
        want []byte
    }{
        {name: "lowercase", give: []byte("hello"), want: []byte("HELLO")},
        {name: "mixed",     give: []byte("HeLLo"), want: []byte("HELLO")},
        {name: "empty",     give: []byte(""),       want: []byte("")},
    }
    h := &uppercaseHandler{}
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            events, err := h.Transform(context.Background(), tt.give, nil)
            require.NoError(t, err)
            require.Len(t, events, 1)
            assert.Equal(t, tt.want, events[0].Value)
        })
    }
}
```

### Mocking transform.Client with gomock

```bash
# Generate mock
mockgen -source=internal/transform/client.go -destination=internal/transform/mock_client_test.go -package=transform
```

```go
func TestPipelineWithMockTransformer(t *testing.T) {
    defer goleak.VerifyNone(t)
    ctrl := gomock.NewController(t)

    mockClient := NewMockClient(ctrl)
    mockClient.EXPECT().
        Transform(gomock.Any(), gomock.Any()).
        DoAndReturn(func(ctx context.Context, req *pb.TransformRequest) (*pb.TransformResponse, error) {
            return &pb.TransformResponse{
                Status: pb.Status_OK,
                Events: []*pb.Event{{Value: bytes.ToUpper(req.Payload)}},
            }, nil
        }).
        AnyTimes()

    r := pipeline.NewRunner()
    r.AddTransformer("uppercase", mockClient, 5*time.Second, 3, 100*time.Millisecond)
    // Add mock sink, push frame, verify output
}
```

### Integration Test with InProcessClient

```go
func TestRunnerWithInProcTransform(t *testing.T) {
    defer goleak.VerifyNone(t)

    r := pipeline.NewRunner()
    cli := transform.NewInProcessClient(&uppercaseHandler{})
    r.AddTransformer("uppercase", cli, 5*time.Second, 3, 100*time.Millisecond)

    // Mock sink to capture output
    ctrl := gomock.NewController(t)
    mockSink := NewMockAdapter(ctrl)

    var captured *pb.Frame
    mockSink.EXPECT().
        Publish(gomock.Any(), gomock.Any()).
        DoAndReturn(func(_ context.Context, f *pb.Frame) error {
            captured = f
            return nil
        }).
        Times(1)

    r.AddSink(mockSink)

    frame := &pb.Frame{Value: []byte("hello")}
    err := r.pushFrame(context.Background(), frame)
    require.NoError(t, err)
    assert.Equal(t, []byte("HELLO"), captured.Value)
}
```

---

## Common Mistakes

1. **Forgetting to close gRPC connections** — Always close in `Client.Close()`.
2. **Blocking in Stream receive loop** — Use select with ctx.Done().
3. **Ignoring GRANT credits** — Sending requests without credits causes plugin overload.
4. **Not preserving checkpoint** — Output frames MUST carry the original checkpoint token.
5. **Mixing sync/async in streaming** — Pick one model per stream, don't mix.
6. **Leaking stream goroutines** — Always cancel stream context on shutdown.
