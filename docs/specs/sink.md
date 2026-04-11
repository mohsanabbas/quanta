# Sink Specification

Sinks implement:

```go
type Adapter interface {
    Configure(ctx context.Context, cfg any) error
    Publish(ctx context.Context, frame *pb.Frame) error
    Close(ctx context.Context) error
}
```

## AckAware Interface

Sinks that confirm delivery asynchronously implement `AckAware`:

```go
type EmitFn func(ctx context.Context, tok *pb.CheckpointToken)

type AckAware interface {
    BindAck(EmitFn)
}
```

During pipeline wiring, `Runner.AddSink` detects `AckAware` sinks and calls `BindAck(coord.Ack)`, binding the sink directly to the `AckCoordinator`. When the sink confirms delivery, it invokes `EmitFn` with the frame's checkpoint token — the coordinator's barrier decrements its refcount and commits when all sinks have acked.

## NackAware Interface

Sinks that can detect per-message delivery failure implement `NackAware`:

```go
type NackFn func(ctx context.Context, frame *pb.Frame, err error)

type NackAware interface {
    BindNack(NackFn)
}
```

During pipeline wiring, `Runner.AddSink` detects `NackAware` sinks and calls `BindNack(coord.Nack)`, binding the sink to the `AckCoordinator`. When a sink permanently fails to deliver a frame, it invokes `NackFn` — the coordinator routes the frame to the engine-managed DLQ sink (if configured) and then acks the checkpoint token so the pipeline keeps flowing.

A sink can implement both `AckAware` and `NackAware`. On success it calls `EmitFn(tok)`; on failure it calls `NackFn(frame, err)`. If no DLQ is configured, the coordinator does not commit the checkpoint token after the nack, so the frame will be redelivered by the source.

### AckAware vs Synchronous Sinks

| Property            | AckAware Sink                         | Synchronous Sink                               |
| ------------------- | ------------------------------------- | ---------------------------------------------- |
| `Publish` behaviour | Non-blocking enqueue                  | Blocking write                                 |
| Ack mechanism       | Calls `EmitFn(tok)` on confirm        | Runner calls `barrier.Complete()` after return |
| Nack mechanism      | Calls `NackFn(frame, err)` on failure | Returns error from `Publish`                   |
| Barrier refs        | `N frames × M ackAware sinks`         | `+1` for all sync sinks combined               |
| Examples            | Kafka, S3                             | Stdout                                         |

## Registration

Drivers register using `sink.Register(sink.Registration{Name: "kafka", New: func() Adapter { ... }, ConfigProto: func() any { return Config{} }})`. The pipeline requests the adapter and hands it a decoded config struct.

## Publish Semantics

- `Publish` must respect cancellation. If the context deadline is exceeded, return `ctx.Err()` and avoid acknowledging the frame.
- The adapter should treat frames as immutable. Any batching must copy payloads/headers.
- **AckAware sinks** attach the checkpoint token to in-flight metadata (e.g., Sarama's `msg.Metadata`) and ack asynchronously when the broker confirms delivery.
- **Synchronous sinks** return from `Publish` once the write is complete; the runner treats the return as implicit acknowledgement.

## Kafka Sink (AckAware + NackAware)

The Kafka sink uses Sarama's `AsyncProducer`:

```mermaid
sequenceDiagram
  participant Runner
  participant KafkaSink
  participant AsyncProducer as Sarama AsyncProducer
  participant Coordinator as AckCoordinator

  Runner->>KafkaSink: Publish(frame)
  KafkaSink->>AsyncProducer: Input() ← msg{Metadata: inflight{frame}}
  Note over KafkaSink: returns immediately (non-blocking)

  AsyncProducer-->>KafkaSink: Successes channel
  KafkaSink->>KafkaSink: ackLoop() extracts inflight
  KafkaSink->>Coordinator: Ack(tok)
```

- `Publish` attaches `&inflight{frame}` as `msg.Metadata` and sends to `prod.Input()`.
- `ackLoop()` goroutine reads both `Successes` and `Errors` channels:
  - **Success:** `ackFromMetadata` extracts the `inflight` struct and calls `EmitFn(ctx, tok)`.
  - **Error:** `nackFromMetadata` extracts the `inflight` struct; if `NackAware` is bound, calls `NackFn(ctx, frame, err)` — otherwise logs the error and withholds the ack for redelivery.
- `Close` calls `AsyncClose()` and blocks on `<-doneCh` until `ackLoop()` drains both channels via `flushErrors`/`flushSuccesses`.

## S3 Sink (AckAware + NackAware)

The S3 sink batches frames and uploads on flush:

- `uploadBatch` collects checkpoint tokens **and frames** from all entries in the batch.
- On **success**, `ackAll()` calls `EmitFn(ctx, tok)` for every token in the batch.
- On **failure** (encode error or `PutObject` error), `nackAll()` calls `NackFn(ctx, frame, err)` for every frame in the batch — routing them to the engine DLQ. If no DLQ is configured, the coordinator withholds the ack for redelivery.

## Ordering & Idempotence

- Drivers should maintain per-partition ordering by serialising writes per key.
- Idempotence is achieved via consistent keys and deterministic payloads. Kafka sink uses the frame key directly.

## Shutdown

- `Close` should flush and release resources. It receives the same root context used to start the pipeline.
- AckAware sinks must drain in-flight acknowledgements before returning from `Close`.
- NackAware sinks must flush pending nacks (e.g., remaining producer errors) before returning from `Close`.
