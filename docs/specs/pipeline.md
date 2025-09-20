# Pipeline Model

## Transformer Chain

- Transformers are invoked sequentially for each frame.
- Each stage receives the frame context and must return a `pb.TransformResponse`.
- Responses with status `OK` carry zero or more events forward; `DROP` filters the frame; `ERROR`/`RETRY` trigger retry logic according to stage configuration.

## Filter Semantics

- A filter returns `DROP` (or emits no events). The runner acknowledges the original frame immediately and stops the chain.
- Filters must be side-effect free aside from emitting metrics/logs.

## Runner Flow

1. Source emits `emit(ctx, frame)`.
2. Runner iterates through transformers.
3. Resulting frames are published to each sink in order-of-registration.
4. When sinks acknowledge, the runner notifies the source (`Ack`).

## Retry Policy

- Each stage defines `retry_attempts` and `backoff_ms`. Retries use the same context with timeout wrappers.
- Exhaustion logs the failure and acknowledges the frame to prevent deadlock. Future DLQ support will capture these events.

## Observability Hooks

- Logging is provided via `internal/logging`. Metrics for publish attempts/errors are introduced alongside sink retry (see future milestones).

