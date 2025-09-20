# Architecture

## Engine Lifecycle

1. **Build** – Configuration is parsed, adapter registrations are resolved, and each source/sink is configured with the root `context.Context`. No goroutines are launched at this stage.
2. **Start** – `Runner.Start(ctx)` starts the source. Frames emitted by the source run through the transformer chain and into sinks synchronously using the same lineage context.
3. **Stop** – Cancellation of the root context shuts down the pipeline. Sinks receive `Close(ctx)` first, followed by transformers, then the source. The transport server drains outstanding RPCs while the runner exits.

## Context Propagation

- The source supplies a context for each emitted frame. Transformers and sinks *must* honour deadlines and cancellations on that context.
- Retries wrap the frame context with `context.WithTimeout`, ensuring cancellation cascades downstream.
- Shutdown is graceful: once the root context is cancelled, blocking operations unblock and return errors.

## Backpressure Semantics

- Source drivers use bounded semaphores (`Controller`) to cap in-flight frames. If the pipeline stalls, sources block before `emit`.
- Sinks publish synchronously; if their internal queues saturate they block until space is freed or context is cancelled.
- Ack handling (`ackTracker`) serialises callbacks to guarantee offsets are committed exactly once and never lost when queues overflow.

## Delivery Guarantees

- Default behaviour: **at-least-once** delivery. Frames are retried until sinks confirm success.
- **At-most-once** is not provided; offsets are never committed before processing completes.
- **Exactly-once** is not guaranteed. Users can approach it with idempotent sinks and E2E mode.
- **E2E Mode** defers offset commits until sinks acknowledge, ensuring upstream brokers are only advanced after complete success.

## Flow Overview

```mermaid
flowchart LR
  kIn["Kafka Broker\n(input)"]
  kOut["Kafka Broker\n(output)"]

  subgraph Engine
    direction LR
    src["Source Adapter\n(sarama)"]
    run["Pipeline Runner\n(transform chain)"]
    snk["Sink Adapter(s)\n(kafka/stdout)"]
    ack["Ack Tracker"]
  end

  kIn --> src
  src --> run
  run -- gRPC --> xform["Transformer Process\n(gRPC)"]
  xform -- events --> run
  run --> snk
  snk --> kOut
  snk -- ack --> ack
  ack -- commit --> src
  run -. stdout .-> snk
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

### Ack & Commit Loop

```mermaid
sequenceDiagram
  participant Sink
  participant AckTracker
  participant Source as Source Adapter
  participant Kafka as Kafka Broker

  Sink->>AckTracker: emit ack(frame checkpoint)
  AckTracker->>Source: callback(ctx, checkpoint)
  Source->>Kafka: Commit offset
  Kafka-->>Source: Commit success
```
