# Source Specification

Sources implement the `Adapter` interface:

```go
type Adapter interface {
    Configure(ctx context.Context, cfg Config) error
    Run(ctx context.Context, emit func(context.Context, *pb.Frame) error) error
    Close(ctx context.Context) error
}
```

## Registration

Drivers register themselves via `source/kafka.Register(kafka.Registration{Name: "sarama", New: func() Adapter { ... }})`. At build time the pipeline asks the registry for the named adapter and configures it.

## Emit Contract

- `emit` must be called synchronously for each inbound message.
- The provided `context.Context` propagates cancellation from the broker (e.g., Sarama session) into the pipeline.
- If `emit` returns an error, the source should stop consuming and propagate the error upstream.

## Blocking & Backpressure

- Sources must acquire inflight capacity (bytes + messages) before calling `emit`. The Kafka driver uses a weighted semaphore sized by `tuning.inflight_bytes` and `tuning.inflight_msgs`.
- During shutdown, sources must honour `ctx.Done()` and stop emitting promptly. Offsets should not be committed after cancellation.

## Offset Handling

- `commit_mode: auto` marks offsets via the broker client immediately after the frame has been delivered to all sinks. No tracker is used in this mode.
- `commit_mode: e2e` waits for sink acknowledgements. Each partition keeps a sliding window (`window_bits`) and only advances when all earlier offsets are acked. Overflow never fast-forwards—if the window fills up the reader pauses until downstream frees space.
- Config invariants enforce `window_bits ≥ 256` and `inflight_msgs ≤ window_bits` so the window cannot be overrun.
