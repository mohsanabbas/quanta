# Error Ownership & DLQ Boundaries

## Principle

Every error in Quanta has exactly one owner. The owner decides the response.
The engine never overrides a plugin's decision; plugins never handle
infrastructure failures.

---

## Ownership Map

```mermaid
flowchart TB
    subgraph PLUGIN["Plugin Domain (Transform)"]
        direction TB
        P1["Business validation errors"]
        P2["Schema rejection"]
        P3["Domain-specific dead-lettering"]
        P4["Plugin-internal retry logic"]
    end

    subgraph ENGINE["Engine Domain (Infrastructure)"]
        direction TB
        E1["gRPC transport failures"]
        E2["Retry exhaustion"]
        E3["Sink delivery failures"]
        E4["DLQ routing for failed deliveries"]
    end

    subgraph SOURCE["Source Domain"]
        direction TB
        S1["Receives ConnectorAck"]
        S2["Commits or withholds checkpoint"]
        S3["Never knows why a commit happened"]
    end

    PLUGIN -->|"Status_OK with DLQ envelope<br/>OR DeadLetterFn callback"| SOURCE
    ENGINE -->|"Nack -> DLQ sink -> commit<br/>OR withhold -> redeliver"| SOURCE

    style PLUGIN fill:#e3f2fd,stroke:#1565c0
    style ENGINE fill:#fce4ec,stroke:#c62828
    style SOURCE fill:#e8f5e9,stroke:#2e7d32
```

---

## Two Error Paths

### Path 1 Transform Errors (Plugin-Owned)

**Owner:** Plugin developer
**Config:** Code-level (`SetDeadLetter(fn)`)
**Interface:** `DeadLetterFn func(stage string, frame *pb.Frame, cause error)`

The plugin has full authority over message validity. When a transform fails,
the engine does not retry to a different sink or route to an engine DLQ.
The message never reaches sinks.

```mermaid
sequenceDiagram
    participant Source
    participant Engine as Runner
    participant Plugin as Transformer
    participant DLFn as DeadLetterFn

    Source->>Engine: frame
    Engine->>Plugin: Transform(request)

    alt Plugin reachable rejects message
        Plugin-->>Engine: Status_ERROR
        Engine->>DLFn: Fail(stage, frame, err)
        Note over DLFn: Plugin developer's callback<br/>Log / alert / external store
        Note over Engine: Frame dropped never reaches sinks
        Engine->>Source: CommitNow ✓ (deterministic failure)

    else Plugin unreachable gRPC timeout after retries
        Plugin--xEngine: Unavailable / DeadlineExceeded
        Engine->>DLFn: Fail(stage, frame, err)
        Note over Engine: Frame dropped never reaches sinks
        Engine->>Source: CommitNow ✓ (retrying is futile)

    else Plugin validates and routes to DLQ itself
        Plugin-->>Engine: Status_OK + DLQ envelope
        Note over Engine: Treats as success normal sink path
        Engine->>Source: commit ✓ (after sink ack)
    end
```

**Why always commit?** Transform failures are deterministic the same message
will fail the same way on redelivery. Withholding would create an infinite
retry loop (poison pill). The plugin had its chance. Advance the source.

**What if `DeadLetterFn` is not set?** The frame is silently dropped and the
source advances. The only trace is `slog.Error`. This is the plugin
developer's responsibility if they care about failed messages, they set the
callback.

---

### Path 2 Sink Errors (Engine-Owned)

**Owner:** Engine operator
**Config:** YAML (`pipeline.yml` -> `dlq:` section)
**Interface:** `NackFn func(ctx context.Context, frame *pb.Frame, err error)`

The frame was valid and transformed successfully. The sink couldn't deliver it.
This is an infrastructure problem the engine owns the response.

```mermaid
sequenceDiagram
    participant Source
    participant Engine as Runner
    participant Coord as AckCoordinator
    participant Sink as Sink (NackAware)
    participant DLQ as DLQ Sink

    Source->>Engine: frame
    Engine->>Engine: transform chain ✓
    Engine->>Coord: Barrier(tok, refs)
    Engine->>Sink: Publish(frame)

    alt Delivery succeeds
        Sink-->>Coord: Ack(ctx, tok)
        Coord->>Source: commit ✓

    else Delivery fails DLQ configured
        Sink-->>Coord: Nack(ctx, frame, err)
        Coord->>Coord: Abort barrier (CAS Live->Aborted)
        Coord->>DLQ: Publish(dlq_frame)
        Note over DLQ: x-dlq-error + x-dlq-original-key headers
        Coord->>Source: commit ✓ pipeline unblocked

    else Delivery fails DLQ publish fails
        Sink-->>Coord: Nack(ctx, frame, err)
        Coord->>Coord: Abort barrier
        Coord->>DLQ: Publish(dlq_frame) ✗
        Note over Coord: Withhold commit safe default
        Source->>Engine: redeliver frame

    else Delivery fails no DLQ configured
        Sink-->>Coord: Nack(ctx, frame, err)
        Coord->>Coord: Abort barrier
        Note over Coord: Withhold commit
        Source->>Engine: redeliver frame
    end
```

**Why conditional commit?** Sink failures are transient the broker might
recover, the network might heal. Redelivery might succeed. The engine only
commits when the DLQ safely captures the frame, preserving at-least-once
delivery.

---

## Boundary Rules

```
┌─────────────────────────────────────────────────────────────────┐
│                        Transform Layer                          │
│                                                                 │
│  Plugin returns Status_OK with DLQ envelope?                    │
│    -> Engine sees SUCCESS. Normal sink path. Plugin's decision.  │
│                                                                 │
│  Plugin returns Status_ERROR / gRPC fails?                      │
│    -> Engine calls Fail(). Frame NEVER reaches sinks.            │
│    -> DeadLetterFn fires if set. Source always advances.         │
│                                                                 │
│  Rule: The engine does not second-guess the plugin.             │
│        If the plugin says ERROR, the message is dead.           │
├─────────────────────────────────────────────────────────────────┤
│                          Sink Layer                             │
│                                                                 │
│  Sink delivers successfully?                                    │
│    -> Ack(ctx, tok). Barrier resolves. Source commits.           │
│                                                                 │
│  Sink fails to deliver?                                         │
│    -> Nack(ctx, frame, err). Barrier aborted.                    │
│    -> DLQ configured? Publish + commit. Not? Withhold.           │
│                                                                 │
│  Rule: The engine owns the DLQ decision for sink failures.      │
│        The sink just reports success or failure.                 │
└─────────────────────────────────────────────────────────────────┘
```

---

## Comparison Table

| Aspect                   | Transform Error Path           | Sink Error Path                 |
| ------------------------ | ------------------------------ | ------------------------------- |
| **Owner**                | Plugin developer               | Engine operator                 |
| **Configuration**        | Code: `SetDeadLetter(fn)`      | YAML: `dlq:` section            |
| **Trigger**              | `Status_ERROR`, gRPC failure   | Sink `Publish()` fails          |
| **Frame reaches sinks?** | Never                          | Yes (already transformed)       |
| **Commit behavior**      | Always advance (deterministic) | Only on DLQ success (transient) |
| **No handler set**       | Silent drop + advance + log    | Withhold + redeliver            |
| **Redelivery useful?**   | No same failure on retry       | Yes infrastructure may recover  |
| **Coordinator method**   | `Fail(stage, frame, cause)`    | `Nack(ctx, frame, cause)`       |

---

## Combined Flow

```mermaid
flowchart TD
    frame["Source Frame"] --> transform["Transform Chain"]

    transform -->|"Status_OK"| sinks["Publish to Sink(s)"]
    transform -->|"Status_DROP"| commitDrop["CommitNow ✓<br/>(intentional filter)"]
    transform -->|"Status_ERROR /<br/>gRPC failure"| fail["coord.Fail()"]

    fail --> dlfn{"DeadLetterFn<br/>set?"}
    dlfn -->|Yes| callback["Callback: log / alert / store"]
    dlfn -->|No| silent["Silent drop + slog.Error"]
    callback --> commitFail["CommitNow ✓<br/>(deterministic, never retry)"]
    silent --> commitFail

    sinks -->|"All ack"| commitOK["Commit ✓"]
    sinks -->|"Nack"| nackPath{"Engine DLQ<br/>configured?"}

    nackPath -->|Yes| dlqPub["DLQ Publish"]
    nackPath -->|No| withhold["Withhold -> redeliver"]

    dlqPub -->|Success| commitDLQ["Commit ✓<br/>(pipeline unblocked)"]
    dlqPub -->|Failure| withhold

    style sinks fill:#e8f5e9
    style fail fill:#fff3e0
    style commitOK fill:#c8e6c9
    style commitDrop fill:#c8e6c9
    style commitFail fill:#c8e6c9
    style commitDLQ fill:#c8e6c9
    style withhold fill:#ffcdd2
    style callback fill:#fff3e0
    style dlqPub fill:#e3f2fd
```

---

## Design Rationale

1. **Two owners, two mechanisms, zero overlap.** Transform errors and sink
   errors have different root causes, different retry semantics, and different
   audiences. Merging them into one path forces a single commit policy on two
   incompatible failure modes.

2. **Plugin authority is absolute.** If a transform says a message is invalid,
   the engine does not preserve it, retry it, or route it to an engine DLQ.
   The plugin can route to its own DLQ (via `Status_OK` + envelope) if it
   wants the message preserved. This is the plugin's domain decision.

3. **Engine DLQ is for infrastructure, not validation.** The `dlq:` config in
   `pipeline.yml` exists solely for frames that were valid but couldn't be
   delivered. Operators configure it to prevent pipeline stalls on broker
   outages or sink failures. It never captures transform rejections.

4. **Commit semantics follow failure type.** Deterministic failures (transform)
   always commit retrying would produce the same error. Transient failures
   (sink) withhold by default the infrastructure may recover. This prevents
   both infinite retry loops and unnecessary data loss.
