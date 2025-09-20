# Frame Specification

`pb.Frame` is the immutable payload that flows through the pipeline.

## Fields & Semantics

| Field | Type | Description |
|-------|------|-------------|
| `Key` | `[]byte` | Routing key for downstream sinks. Transformers may override by setting `metadata.Attributes["sink.key"]`. |
| `Value` | `[]byte` | Raw payload. Treated as opaque by the runner. |
| `Headers` | `map[string][]byte` | Optional metadata. Header keys must be valid UTF-8. Values can be arbitrary bytes. |
| `Ts` | `*timestamppb.Timestamp` | Optional event timestamp. |
| `Checkpoint` | `*pb.CheckpointToken` | Source-specific acknowledgement token (e.g., Kafka topic/partition/offset). |

## Mutation Rules

- Frames supplied by sources must not be mutated after emission.
- Transformers must allocate new frames when altering payload, headers, or metadata. Helpers (`toFrames`) clone header maps and timestamps.
- Sinks treat frames as read-only.

## Headers & Attributes

- Headers are copied verbatim to sinks. Non-printable values should be base64 encoded by observers when needed.
- Metadata attributes are used for routing hints: `sink.key`, `trace.id`, `route.*`, etc.

## Key Handling

- Initial key comes from the source (Kafka message key when available).
- Transformers can steer output partitions by providing a new key via metadata.
- Sinks rely on keys for idempotence and ordering; drivers must honour the frame key when publishing.

