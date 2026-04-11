# Error Handling

## Error Classification

| Stage Outcome                     | Commit Offset? | Retry?                       | Dead-Letter?               | Notes                                                                          |
| --------------------------------- | -------------- | ---------------------------- | -------------------------- | ------------------------------------------------------------------------------ |
| Transform success → All sinks ack | Yes            | No                           | No                         | Barrier refs reach 0 → `Live→Committed`.                                       |
| Transform transient error         | No (pending)   | Yes, bounded by stage config | No                         | Retried with backoff. After exhaustion → permanent failure.                    |
| Transform permanent error         | Conditional    | No                           | Yes (engine DLQ sink)      | `Fail()` dead-letters. If no frames survive, `CommitNow()` advances offset.    |
| Sink publish error (sync)         | No             | No (barrier aborted)         | No                         | `barrier.Abort()` prevents commit. Offset withheld; source redelivers.         |
| NackAware sink delivery failure   | Yes            | No                           | Yes (engine DLQ sink)      | Nack → coordinator publishes to DLQ sink → barrier completes → offset commits. |
| AckAware sink delivery failure    | No             | Broker-level                 | No                         | Withhold ack → barrier stays Live → offset never committed → redelivery.       |
| Plugin rejects event              | Yes            | No                           | Yes (per-stage error sink) | Plugin returns `error_events` → engine routes to configured `error_sink`.      |
| Transformer `DROP`                | Yes            | No                           | No                         | No derived frames → `CommitNow()`.                                             |
| Context cancelled                 | No             | No                           | No                         | Outstanding barriers abandoned. Shutdown safety.                               |

---

## Error Ownership Model

> See [Error Ownership](error-ownership.md) for the full three-path design.

There are **three separate error-handling paths** in Quanta, each owned by a
different component. See `docs/specs/error-ownership.md` for the definitive
reference.

### 1. Plugin Error Routing (`error_events` → `error_sink`)

When a transformer decides a message is invalid — bad schema, missing
fields, business rejection — the plugin returns the rejected events in
`TransformResponse.error_events`. The engine routes them to the transformer's
configured `error_sink`.

```yaml
transformers:
  - name: cloudevents
    type: grpc
    address: localhost:50052
    error_sink:
      sink: kafka
      config:
        topic: quanta-error-events
        brokers: ["localhost:9092"]
```

```mermaid
sequenceDiagram
  participant Source
  participant Engine as Runner (Engine)
  participant Plugin as Transformer Plugin
  participant Sink as Configured Sink(s)
  participant ESink as Error Sink

  Source->>Engine: frame (invalid payload)
  Engine->>Plugin: Transform(request)
  Note over Plugin: Validates payload → invalid
  Plugin->>Plugin: Build error event
  Plugin-->>Engine: TransformResponse{Status: OK, Events: [valid], ErrorEvents: [rejected]}
  Engine->>Sink: Publish(valid frames)
  Engine->>ESink: Publish(rejected frames)
  Sink-->>Engine: ack
  Engine->>Source: commit offset ✓
```

**Key characteristics:**

- Plugin returns `Status_OK` with valid events in `events` and rejected events in `error_events`
- Engine routes `error_events` to the per-stage `error_sink` (configured in pipeline YAML)
- From the engine's perspective, the transform **succeeded**
- **Offset commits** — the message was successfully processed
- If no `error_sink` is configured, error events are logged and dropped

**Example — CloudEvents transformer:**

```go
func (s *transformerServer) Transform(ctx context.Context, req *pb.TransformRequest) (*pb.TransformResponse, error) {
    ce, err := toCloudEvent(req.Frame.Value)
    if err != nil {
        // Return as error_event — engine routes to error_sink
        return toErrorEvent(req.Frame.Value, "parse_error", err.Error()), nil
    }
    return toSuccess(ce), nil
}

func toErrorEvent(raw []byte, errClass, errMsg string) *pb.TransformResponse {
    envelope, _ := json.Marshal(errorEnvelope{Error: errMsg, ...})
    return &pb.TransformResponse{
        Status: pb.Status_OK,
        ErrorEvents: []*pb.Event{{Value: envelope}},
    }
}
```

### 2. Engine DLQ Sink (Sink Delivery Failures)

When a NackAware sink permanently fails to deliver a frame (e.g., Kafka broker
rejects, S3 upload fails), it calls its `NackFn`. The `AckCoordinator`
publishes the failed frame to the engine-managed DLQ sink and commits the
offset so the source advances.

```yaml
dlq:
  enabled: true
  sink: kafka
  config:
    topic: quanta-engine-dlq
    brokers: ["localhost:9092"]
  include_original_headers: true
  include_error_metadata: true
```

```mermaid
sequenceDiagram
  participant Source
  participant Engine as Runner (Engine)
  participant Sink as NackAware Sink
  participant DLQ as Engine DLQ Sink

  Source->>Engine: frame
  Engine->>Sink: Publish(frame)
  Sink--xEngine: Nack(frame, err)
  Note over Engine: AckCoordinator.Nack()
  Engine->>Engine: barrier.Abort()
  Engine->>DLQ: Publish(dlq_frame)
  DLQ-->>Engine: ack
  Engine->>Source: commit offset ✓
```

**Key characteristics:**

- Triggered by `NackFn` callback from NackAware sinks (Kafka, S3)
- `AckCoordinator.Nack()` aborts the barrier, publishes to DLQ, conditionally commits
- The DLQ sink is configured at the pipeline level (not per-transformer)
- If no DLQ is configured, the nack withholds the offset and logs a warning
- Offset commits after DLQ publish — the message is not redelivered

### 3. Engine DeadLetterFn (Transform Infrastructure Failures)

When the transform infrastructure itself fails — gRPC timeout, connection
refused, all retries exhausted — the engine invokes `DeadLetterFn` as a
**last resort**. This is not a business decision; it means the plugin was
unreachable.

```mermaid
sequenceDiagram
  participant Source
  participant Engine as Runner (Engine)
  participant Plugin as Transformer Plugin
  participant DLFn as DeadLetterFn

  Source->>Engine: frame
  Engine->>Plugin: Transform(request)
  Plugin--xEngine: gRPC Unavailable
  Note over Engine: Retry 1, 2, 3... exhausted
  Engine->>Engine: handlePermanentFailure()
  Engine->>DLFn: Fail(stage, frame, err)
  Note over DLFn: Log / external sink / alert
  Note over Engine: No frames survived → CommitNow(tok)
  Engine->>Source: commit offset ✓
```

**Key characteristics:**

- Triggered by `AckCoordinator.Fail(stage, frame, cause)` after retry exhaustion
- `DeadLetterFn` is a callback — the engine does not dictate where the dead letter goes
- The checkpoint decision is made by `pushFrame()`, not by `DeadLetterFn`:
  - If **no frames survived** the transform chain → `CommitNow(tok)` → offset advances
  - If **some frames survived** → barrier covers the survivors → offset commits when sinks ack

```go
type DeadLetterFn func(stage string, frame *pb.Frame, cause error)
```

- Set via `Runner.SetDeadLetter(fn)` → `coord.SetDeadLetter(fn)`
- The handler is free to log, push to an external queue, send an alert — the engine doesn't care
- The handler does **not** control offset commit behaviour

### Comparison

| Aspect               | Plugin Error Routing                           | Engine DLQ Sink                                | Engine DeadLetterFn                               |
| -------------------- | ---------------------------------------------- | ---------------------------------------------- | ------------------------------------------------- |
| **Who decides?**     | Transformer plugin (business logic)            | Engine (sink delivery failure)                 | Engine (infrastructure failure)                   |
| **When triggered?**  | Plugin validates payload and rejects it        | NackAware sink fails to deliver                | gRPC error / timeout after all retries            |
| **Transform status** | `Status_OK` (success with error_events)        | N/A (post-transform)                           | No response (transport failure)                   |
| **Offset commits?**  | Yes — engine sees it as a successful transform | Yes — after DLQ publish                        | Conditional — depends on surviving frames         |
| **Redelivery?**      | No — message was processed successfully        | No — DLQ captures the failure                  | No — engine calls `CommitNow` if nothing survived |
| **Destination**      | Per-transformer `error_sink`                   | Pipeline-level DLQ sink                        | Caller-provided callback (external to engine)     |
| **Sink involved?**   | Yes — error_events flow to error_sink adapter  | Yes — DLQ frame flows through DLQ sink adapter | No — `DeadLetterFn` is a direct callback          |

### Error Flow Diagram (Combined)

```mermaid
flowchart TD
  frame["Source Frame"] --> transform["Transform Chain"]

  transform -->|"Status_OK<br/>(valid output)"| publish["publishAll → sinks"]
  transform -->|"Status_OK<br/>(error_events)"| errorSink["publishErrorEvents → error_sink"]
  transform -->|"Status_DROP"| commitNow["CommitNow(tok)"]
  transform -->|"gRPC error<br/>after retries"| fail["coord.Fail()"]

  publish -->|success| ackWait["Wait for sink acks"]
  publish -->|sync error| abort["barrier.Abort()"]
  publish -->|"nack<br/>(NackAware)"| nack["coord.Nack()"]

  ackWait -->|"all refs=0"| commit["Commit offset ✓"]
  abort --> withhold["Offset withheld<br/>source redelivers"]
  nack -->|"DLQ configured"| dlqPublish["DLQ sink publish"]
  nack -->|"no DLQ"| withhold
  dlqPublish --> commit

  errorSink --> commit

  fail --> dlfn["DeadLetterFn callback"]
  dlfn --> commitCheck{"Any frames<br/>survived?"}
  commitCheck -->|No| commitNow2["CommitNow(tok) ✓"]
  commitCheck -->|Yes| barrierWait["Surviving barrier<br/>resolves normally"]

  style publish fill:#e8f5e9
  style errorSink fill:#fff3e0
  style fail fill:#ffebee
  style commit fill:#c8e6c9
  style commitNow fill:#c8e6c9
  style commitNow2 fill:#c8e6c9
  style withhold fill:#ffcdd2
  style dlfn fill:#fff3e0
  style dlqPublish fill:#e8f5e9
  style nack fill:#ffebee
```

> **Rule of thumb:**
>
> - Plugin can parse the message but rejects it → return in `error_events` (Path 1)
> - Sink fails to deliver → NackAware sink nacks → engine DLQ (Path 2)
> - Plugin unreachable → engine `DeadLetterFn` (Path 3)

---

## Error Flow (Sink Delivery)

```mermaid
flowchart TD
  subgraph "Sync Sink (e.g., stdout)"
    syncPub["Publish()"] -->|error| syncAbort["barrier.Abort()<br/>offset withheld"]
    syncPub -->|ok| syncComplete["barrier.Complete()"]
  end

  subgraph "NackAware Sink (e.g., Kafka, S3)"
    nackPub["Publish() → enqueue"] -->|"delivery failure"| nack["Nack(frame, err)"]
    nackPub -->|"broker confirms"| nackAck["Ack(tok)<br/>barrier.Complete()"]
    nack --> coordNack["coord.Nack()<br/>barrier.Abort()"]
    coordNack -->|"DLQ configured"| dlq["DLQ sink publish<br/>offset commits ✓"]
    coordNack -->|"no DLQ"| nackWithhold["Offset withheld<br/>source redelivers"]
  end

  subgraph "AckAware-only Sink (legacy)"
    asyncPub["Publish() → enqueue"] -->|"broker confirms"| ack["Ack(tok)<br/>barrier.Complete()"]
    asyncPub -->|"broker rejects"| withhold["Withhold ack<br/>barrier stays Live"]
    withhold --> noCommit["Offset never committed<br/>source redelivers on restart"]
  end
```

## Hooks

- Stage configuration controls retry count and backoff duration per transformer.
- Sink adapters expose retry/backoff knobs (`retry.*`) in their config.
- Metrics: publish attempts/errors, retry totals, barrier commit/abort counts via `AckCoordinator.Len()` observability.
