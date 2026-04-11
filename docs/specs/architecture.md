# Architecture

## Engine Lifecycle

1. **Build** – Configuration is parsed, adapter registrations are resolved, and each source/sink is configured with the root `context.Context`. The `AckCoordinator` is created and wired into the `Runner`. No goroutines are launched at this stage.
2. **Start** – `Runner.Start(ctx)` starts the source. Frames emitted by the source run through the transformer chain and into sinks. `AckAware` sinks (Kafka, S3) publish asynchronously and call back through the coordinator when delivery is confirmed.
3. **Stop** – Cancellation of the root context shuts down the pipeline. Sinks receive `Close(ctx)` first, followed by transformers, then the source. The transport server drains outstanding RPCs while the runner exits. Outstanding barriers are abandoned (no commit).

## Context Propagation

- The source supplies a context for each emitted frame. Transformers and sinks _must_ honour deadlines and cancellations on that context.
- Retries wrap the frame context with `context.WithTimeout`, ensuring cancellation cascades downstream.
- Shutdown is graceful: once the root context is cancelled, blocking operations unblock and return errors.

## Backpressure Semantics

- Source drivers use bounded semaphores (`Controller`) to cap in-flight frames. If the pipeline stalls, sources block before `emit`.
- `AckAware` sinks (Kafka, S3) publish non-blocking; backpressure propagates through the coordinator's outstanding barrier count.
- Synchronous sinks (stdout) complete inline — the barrier receives its `Complete()` call immediately after `Publish` returns.

## Delivery Guarantees

- Default behaviour: **at-least-once** delivery. Frames are retried until sinks confirm success.
- **At-most-once** is not provided; offsets are never committed before processing completes.
- **Exactly-once** is not guaranteed. Users can approach it with idempotent sinks and E2E mode.
- **E2E Mode** defers offset commits until **all** sinks acknowledge via the `AckCoordinator` barrier, ensuring upstream brokers are only advanced after complete success.

## AckCoordinator

The `AckCoordinator` is the **sole commit authority** in the pipeline. No component ever commits a checkpoint directly — all commits flow through the coordinator.

### Design

Inspired by the Linux kernel's `kref` refcounting pattern and Kafka's group-coordinator protocol:

- **Barrier** — a refcounted completion token created per source frame. The reference count equals `len(derivedFrames) × ackAwareSinks + 1` (the `+1` accounts for synchronous sinks).
- **Tri-state CAS** — each barrier transitions through exactly one of three terminal states: `Live → Committed` or `Live → Aborted`. A single `CompareAndSwap` arbitrates the race between `release()` and `Abort()`.
- **Pointer-safe removal** — `removeBarrier` compares the map entry's pointer identity before deleting, preventing a successor barrier from being accidentally clobbered by its predecessor's cleanup.

### Barrier State Machine

```mermaid
stateDiagram-v2
  [*] --> Live : Barrier(tok, refs)
  Live --> Committed : refs ≤ 0 (CAS succeeds)
  Live --> Aborted : Abort() CAS succeeds
  Committed --> [*] : commit(tok) → source
  Aborted --> [*] : removeBarrier (no commit)
```

### Data Flow

```mermaid
flowchart LR
  kIn["Kafka Broker\n(input)"]
  kOut["Kafka Broker\n(output)"]
  s3["S3 Bucket"]

  subgraph Engine
    direction LR
    src["Source Adapter\n(sarama)"]
    run["Pipeline Runner\n(transform chain)"]
    coord["AckCoordinator\n(barrier map)"]
    snk_k["Kafka Sink\n(AckAware)"]
    snk_s3["S3 Sink\n(AckAware)"]
    snk_out["Stdout Sink\n(sync)"]
  end

  kIn --> src
  src --> run
  run -- "gRPC" --> xform["Transformer\n(gRPC)"]
  xform -- "events" --> run

  run -- "Barrier(tok, refs)" --> coord
  run --> snk_k
  run --> snk_s3
  run -.-> snk_out

  snk_k -- "Ack(tok)" --> coord
  snk_s3 -- "Ack(tok)" --> coord
  snk_out -- "Complete()" --> coord

  snk_k --> kOut
  snk_s3 --> s3

  coord -- "commit → Subscribe" --> src
```

### Coordinator Sequence

```mermaid
sequenceDiagram
  participant Source
  participant Runner
  participant Coordinator as AckCoordinator
  participant KafkaSink as Kafka Sink (AckAware)
  participant S3Sink as S3 Sink (AckAware)

  Source->>Runner: emit(ctx, frame)
  Runner->>Runner: run transform chain
  Runner->>Coordinator: Barrier(tok, refs=3)
  Runner->>KafkaSink: Publish(frame₁)
  Runner->>S3Sink: Publish(frame₁)
  Note over KafkaSink: non-blocking enqueue

  KafkaSink-->>Coordinator: Ack(tok) [on producer success]
  S3Sink-->>Coordinator: Ack(tok) [on batch upload]
  Runner->>Coordinator: Complete() [sync sinks done]

  Note over Coordinator: refs=0, CAS Live→Committed
  Coordinator->>Source: Subscribe callback → commit offset
```

### Transformer RPC Modes

```mermaid
sequenceDiagram
  participant Runner
  participant UnaryClient as Unary gRPC Client
  participant Transformer

  Runner->>UnaryClient: Transform(ctx, request)
  UnaryClient->>Transformer: Transform(request)
  Transformer-->>UnaryClient: TransformResponse(events)
  UnaryClient-->>Runner: TransformResponse
```

```mermaid
sequenceDiagram
  participant Runner
  participant StreamClient as Stream gRPC Client
  participant Transformer

  Runner->>StreamClient: TransformStream(ctx)
  StreamClient->>Transformer: send frame batch
  Transformer-->>StreamClient: stream responses
  StreamClient-->>Runner: events / control
```

### Error & Abort Flow

```mermaid
sequenceDiagram
  participant Runner
  participant Coordinator as AckCoordinator
  participant DeadLetter as DeadLetterFn

  Runner->>Coordinator: Barrier(tok, refs=2)
  Runner->>Runner: publishAll fails
  Runner->>Coordinator: barrier.Abort()
  Note over Coordinator: CAS Live→Aborted, no commit
  Runner->>Coordinator: Fail(stage, frame, err)
  Coordinator->>DeadLetter: fn(stage, frame, err)
```
