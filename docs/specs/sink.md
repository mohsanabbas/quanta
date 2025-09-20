# Sink Specification

Sinks implement:

```go
type Adapter interface {
    Configure(ctx context.Context, cfg any) error
    Publish(ctx context.Context, frame *pb.Frame) error
    Close(ctx context.Context) error
}
```

## Registration

Drivers register using `sink.Register(sink.Registration{Name: "kafka", New: func() Adapter { ... }, ConfigProto: func() any { return Config{} }})`. The pipeline requests the adapter and hands it a decoded config struct.

## Publish Semantics

- `Publish` must respect cancellation. If the context deadline is exceeded, return `ctx.Err()` and avoid acknowledging the frame.
- The adapter should treat frames as immutable. Any batching must copy payloads/headers.
- On success, `AckAware` sinks invoke the provided ack callback.

## Ordering & Idempotence

- Drivers should maintain per-partition ordering by serialising writes per key.
- Idempotence is achieved via consistent keys and deterministic payloads. Kafka sink uses the frame key directly.

## Shutdown

- `Close` should flush and release resources. It receives the same root context used to start the pipeline.

