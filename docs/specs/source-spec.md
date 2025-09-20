# Source Specification

Sources implement the `source/kafka.Adapter` interface:

```go
Configure(ctx context.Context, cfg Config) error
Run(ctx context.Context, emit func(context.Context, *pb.Frame) error) error
Close(ctx context.Context) error
```

## Lifecycle

1. `Configure` is invoked during pipeline build with the global engine context. Implementations should allocate clients and verify connectivity.
2. `Run` starts consumption. It must respect `ctx.Done()` and stop emitting promptly during cancellation.
3. `emit(runCtx, frame)` must be called sequentially for each record. `runCtx` is the source’s best effort representation of the upstream record’s lifecycle (e.g., Sarama session context). Sources may block before calling `emit` while waiting for backpressure tokens.
4. `Close` releases resources after cancellation.

## Kafka Sarama Driver

- Uses `Controller` and `Manager` for backpressure and checkpoint tracking.
- Only commits offsets after the sink acknowledges (E2E mode). Auto-commit mode commits immediately after transform.
- Ack tracker ensures callbacks are executed even under queue saturation; it never drops acknowledgements silently.

## Emit Semantics

- `emit` must be synchronous: only return after the frame has been handed to the runner. Errors from the runner should propagate back to the source to allow controlled shutdown.
- Sources must not mutate frames after emitting them.

## Offset / Ack Policy

- On `CommitAuto`, offsets are committed after the transform stage. On `CommitE2E`, offsets are committed only after sink acknowledgement.
- On errors before acknowledgement, the checkpoint remains pending. The tracker will eventually dispatch the ack callback once the sink or DLQ confirms completion.

