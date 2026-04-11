# Error Handling

## Error Classification

| Stage Outcome                     | Commit Offset? | Retry?                       | Dead-Letter?     | Notes                                                                          |
| --------------------------------- | -------------- | ---------------------------- | ---------------- | ------------------------------------------------------------------------------ |
| Transform success → All sinks ack | Yes            | No                           | No               | Barrier refs reach 0 → `Live→Committed`.                                       |
| Transform transient error         | No (pending)   | Yes, bounded by stage config | No               | Retried with backoff. After exhaustion → permanent failure.                    |
| Transform permanent error         | Conditional    | No                           | Yes (engine DLQ) | `Fail()` dead-letters. If no frames survive, `CommitNow()` advances offset.    |
| Sink publish error                | No             | No (barrier aborted)         | No               | `barrier.Abort()` prevents commit. Offset withheld; source redelivers.         |
| AckAware sink delivery failure    | No             | Broker-level                 | No               | Pump withholds ack → barrier stays Live → offset never committed → redelivery. |
| Transformer `DROP`                | Yes            | No                           | No               | No derived frames → `CommitNow()`.                                             |
| Transformer routes to DLQ topic   | Yes            | No                           | Yes (plugin DLQ) | Plugin returns `Status_OK` with DLQ envelope → engine treats as success.       |
| Context cancelled                 | No             | No                           | No               | Outstanding barriers abandoned. Shutdown safety.                               |

---

## DLQ Ownership Model

> **Critical design principle:** Quanta does not own the dead-letter queue.
> The transformer plugin owns DLQ routing decisions. The engine provides a
> last-resort `DeadLetterFn` only for infrastructure failures.

There are **two completely separate DLQ mechanisms** in Quanta, and confusing
them leads to incorrect assumptions about offset commit behaviour.

### 1. Plugin-Owned DLQ (Business Logic)

When a transformer decides a message is invalid — bad schema, missing
fields, rejected status — the plugin itself routes the event to a DLQ topic.
**The engine never sees this as a failure.**

```mermaid
sequenceDiagram
  participant Source
  participant Engine as Runner (Engine)
  participant Plugin as Transformer Plugin
  participant Sink as Configured Sink(s)
  participant DLQ as DLQ Topic

  Source->>Engine: frame (invalid payload)
  Engine->>Plugin: Transform(request)
  Note over Plugin: Validates payload → invalid
  Plugin->>Plugin: Build DLQ envelope
  Plugin-->>Engine: TransformResponse{Status: OK, Events: [dlq_frame]}
  Note over Engine: Status=OK → treat as success
  Engine->>Sink: Publish(dlq_frame)
  Note over Sink: dlq_frame has header "__topic"="quanta-dlq"
  Sink-->>Engine: ack
  Engine->>Source: commit offset ✓
```

**Key characteristics:**

- The plugin returns `Status_OK` with a DLQ envelope as the output event
- From the engine's perspective, the transform **succeeded** — it got a valid frame back
- The DLQ routing happens via header-based topic override (e.g., `__topic: quanta-dlq`)
- The sink publishes to the DLQ topic, acks, barrier completes, **offset commits**
- The source message is **not redelivered** — it was successfully processed

**Example — CloudEvents transformer:**

```go
// The plugin decides this message is invalid and routes to DLQ
func (s *transformerServer) toDLQ(raw []byte, errClass, errMsg string) *pb.TransformResponse {
    envelope := dlqEnvelope{
        Error:       errMsg,
        ErrorClass:  errClass,
        Transformer: _pluginName,
        RawPayload:  rawPayload,
    }
    payload, _ := json.Marshal(envelope)

    // Route to DLQ via header-based topic override
    headers := map[string]string{
        "dlq-error":       errMsg,
        "__topic":         "quanta-dlq",  // ← sink picks this up
    }

    return &pb.TransformResponse{
        Status: pb.Status_OK,          // ← engine sees SUCCESS
        Events: []*pb.Event{{Value: payload, Metadata: &pb.EventMetadata{Headers: headers}}},
    }
}
```

### 2. Engine-Owned DeadLetterFn (Infrastructure Failures)

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

| Aspect               | Plugin-Owned DLQ                               | Engine DeadLetterFn                               |
| -------------------- | ---------------------------------------------- | ------------------------------------------------- |
| **Who decides?**     | Transformer plugin (business logic)            | Engine (infrastructure failure)                   |
| **When triggered?**  | Plugin validates payload and rejects it        | gRPC error / timeout after all retries            |
| **Transform status** | `Status_OK` (success with DLQ frame)           | No response (transport failure)                   |
| **Offset commits?**  | Yes — engine sees it as a successful transform | Conditional — depends on surviving frames         |
| **Redelivery?**      | No — message was processed successfully        | No — engine calls `CommitNow` if nothing survived |
| **DLQ destination**  | Plugin-chosen topic via header routing         | Caller-provided callback (external to engine)     |
| **Sink involved?**   | Yes — DLQ frame flows through configured sinks | No — `DeadLetterFn` is a direct callback          |

### DLQ Flow Diagram (Combined)

```mermaid
flowchart TD
  frame["Source Frame"] --> transform["Transform Chain"]

  transform -->|"Status_OK<br/>(valid output)"| publish["publishAll → sinks"]
  transform -->|"Status_OK<br/>(DLQ envelope)"| publish
  transform -->|"Status_DROP"| commitNow["CommitNow(tok)"]
  transform -->|"gRPC error<br/>after retries"| fail["coord.Fail()"]

  publish -->|success| ackWait["Wait for sink acks"]
  publish -->|error| abort["barrier.Abort()"]

  ackWait -->|"all refs=0"| commit["Commit offset ✓"]
  abort --> withhold["Offset withheld<br/>source redelivers"]

  fail --> dlfn["DeadLetterFn callback"]
  dlfn --> commitCheck{"Any frames<br/>survived?"}
  commitCheck -->|No| commitNow2["CommitNow(tok) ✓"]
  commitCheck -->|Yes| barrierWait["Surviving barrier<br/>resolves normally"]

  style publish fill:#e8f5e9
  style fail fill:#ffebee
  style commit fill:#c8e6c9
  style commitNow fill:#c8e6c9
  style commitNow2 fill:#c8e6c9
  style withhold fill:#ffcdd2
  style dlfn fill:#fff3e0
```

> **Rule of thumb:** If the transformer can parse the message and decide it's
> invalid, the transformer should route it to a DLQ topic itself (returning
> `Status_OK`). The engine's `DeadLetterFn` is only for cases where the
> transformer was never reachable in the first place.

---

## Error Flow (Sink Delivery)

```mermaid
flowchart TD
  subgraph "Sync Sink (e.g., stdout)"
    syncPub["Publish()"] -->|error| syncAbort["barrier.Abort()<br/>offset withheld"]
    syncPub -->|ok| syncComplete["barrier.Complete()"]
  end

  subgraph "AckAware Sink (e.g., Kafka, S3)"
    asyncPub["Publish() → enqueue"] -->|"broker confirms"| ack["Ack(tok)<br/>barrier.Complete()"]
    asyncPub -->|"broker rejects"| withhold["Withhold ack<br/>barrier stays Live"]
    withhold --> noCommit["Offset never committed<br/>source redelivers on restart"]
  end
```

## Hooks

- Stage configuration controls retry count and backoff duration per transformer.
- Sink adapters expose retry/backoff knobs (`retry.*`) in their config.
- Metrics: publish attempts/errors, retry totals, barrier commit/abort counts via `AckCoordinator.Len()` observability.
