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

- Sources may block before calling `emit` to respect the runner’s backpressure controller.
- During shutdown, sources must honour `ctx.Done()` and stop emitting promptly. Offsets should not be committed after cancellation.

## Offset Handling

- `commit_mode: auto` commits after transform succeeds.
- `commit_mode: e2e` commits only after sink acknowledgement.
- Ack tracker ensures callbacks are executed in order and never dropped even if the queue is full.

