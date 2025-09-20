# Sink Specification

Sinks implement the `sink.Adapter` interface:

```go
Configure(ctx context.Context, cfg any) error
Publish(ctx context.Context, frame *pb.Frame) error
Close(ctx context.Context) error
```

## Responsibilities

- Honour the provided context for timeouts and cancellation while publishing.
- Decode configuration structs provided during build. Configuration decoding happens in the pipeline using strongly typed config structs.
- Emit acknowledgements by invoking the bound `sink.EmitFn` (for AckAware sinks) when a frame has been durably written.

## Kafka Sink

- Converts `pb.Frame` headers into Sarama record headers.
- Uses the frame key directly; transformers may override it via metadata.
- Publishes synchronously; if the context is cancelled while waiting for the producer queue, `Publish` returns `ctx.Err()`.
- Success and error channels are drained by an internal goroutine; success triggers acknowledgement.

## Stdout Sink

- Performs optional delay, logging, and acknowledgement through `Config` fields.
- Designed for testing; not suitable for production pipelines.

## Future Extensions

- Retry and DLQ hooks are documented in the error-handling section. Current implementation provides the scaffolding for context-aware retries; a future driver can wrap the `Publish` call with backoff using the same interface.

