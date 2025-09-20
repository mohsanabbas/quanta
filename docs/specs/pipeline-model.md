# Pipeline Model

## Lifecycle

A pipeline is instantiated by the engine from configuration and progresses through three phases:

1. **Build** – adapters are discovered via package `init` registrations. Configuration is decoded into strongly typed `Config` structs for sources, sinks and transformers. All components are handed the engine context during `Configure`.
2. **Start** – the engine calls `Runner.Start(ctx)` which in turn starts the source. The source is responsible for emitting frames through `emit(ctx, *pb.Frame)`. Transformers and sinks operate synchronously within the runner using the same lineage context so cancellations propagate downstream.
3. **Stop** – cancellation of the root context triggers shutdown. The runner stops sinks first (`Adapter.Close(ctx)` in registration order), then closes transformers, and finally the source (`Adapter.Close(ctx)`). The transport server is stopped in parallel but only returns after the runner exits.

## Context Propagation

- Each emitted frame carries the `context.Context` provided by the source. Transformers must honour deadlines and cancellations from that context. Sinks must respect the same context in `Publish` so that shutdown and timeouts do not block forever.
- The engine ensures that any derived contexts created for retries are children of the original emission context, preserving cancellation semantics.

## Backpressure and Admission Control

- Sources may block before emitting when the runner pipeline is saturated. The Sarama source uses `Controller.TryAcquire` to limit outstanding frames; release happens after the sink or acknowledgement path completes.
- If a sink cannot drain, pushes will block on its internal queue; cancellation is used to break these waits during shutdown.

## Delivery Semantics Vocabulary

- **At-least-once** – the default behaviour: an event is retried until a sink acknowledges success. Duplicates may appear if a transformer or sink is not idempotent.
- **At-most-once** – not offered. Offsets are not committed before the processing chain finishes.
- **Exactly-once** – not promised. Users can approach it by making sinks idempotent (using keys) and enabling E2E mode.
- **E2E Mode** – when enabled, the source commits offsets only after the sink succeeds. Transform or sink errors prevent commits; filters count as success and commit immediately.
