# Frame Specification

A frame (`pb.Frame`) is the immutable container used internally by the pipeline to shuttle payloads and metadata.

## Fields

| Field         | Type                 | Description |
|---------------|----------------------|-------------|
| `Key`         | `[]byte`             | Routing key forwarded to sinks. Transformers may override via metadata attribute `sink.key`. |
| `Value`       | `[]byte`             | Payload bytes. Treated as opaque by the runner. |
| `Headers`     | `map[string][]byte`  | Optional headers. Keys must be valid UTF-8; values may be arbitrary bytes. |
| `Ts`          | `*timestamppb.Timestamp` | Optional timestamp. |
| `Checkpoint`  | `*pb.CheckpointToken` | Source-specific acknowledgement token (Kafka topic/partition/offset). |

## Mutation Policy

- Frames created by the source must not be mutated in place by transformers or sinks. Transformers create new frames using helper functions (`toFrames`) that copy headers and metadata when needed.
- Headers and attributes are copied lazily; if a transformer updates the map, it should clone it to avoid modifying shared state.

## Key Guidance

- Keys drive partitioning and idempotence for sinks such as Kafka. Transformers should set `metadata.Attributes["sink.key"]` when determining a new key.
- Sources populate the initial key from the inbound record (e.g., Kafka message key).

## Attributes

Attributes live inside transformer metadata. Key conventions:

- `sink.key` – override the frame key before hitting sinks.
- `trace.id`, `trace.span` – optional tracing fields.
- `route.*` – hints used by sinks with templates.

## Headers

- Keys are case-insensitive for routing but preserved as provided.
- Binary values are allowed. When serialised (e.g., for docs or metrics), non-printable values should be base64 encoded.

