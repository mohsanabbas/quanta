# Processor / Transformer Specification

Transformers implement `internal/transform.Client` and are called sequentially for each frame.

## Signature

- Unary call: `Transform(ctx context.Context, req *pb.TransformRequest) (*pb.TransformResponse, error)`.
- Streaming is reserved for future use; implementors should return `Unimplemented` unless opting in.

## Behaviour

- Transformers must be pure with respect to input frame: they either return new events or an explicit status.
- They may emit zero or more events. Each event may carry new metadata (key, headers, timestamp).
- Deterministic output is strongly recommended to avoid non-repeatable retries.

## Status Codes

| Status | Meaning | Engine Action |
|--------|---------|---------------|
| `OK`   | Success | Continue with returned events. |
| `DROP` | Intentional drop | Acknowledge the original frame and do not push to sinks. |
| `RETRY` / `ERROR` | Recoverable vs terminal error | Runner retries within configured budget; after exhaustion, frame is acknowledged (E2E mode may push to DLQ once available). |

## Metadata Handling

- Transformers should carry forward original metadata when not overriding it.
- To change the sink key, set `event.Metadata.Attributes["sink.key"]`. The runner applies it before sending to sinks.
- Headers set on events are propagated verbatim to sinks. To remove headers, omit them in the output event.

## Error Contract

- Returning a non-nil `error` equates to a recoverable failure; retry policy defines backoff.
- Returning a `TransformResponse` with status `ERROR`/`RETRY` and optional `RetryAfterMs` allows explicit control without raising Go errors.

