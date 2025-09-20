# Kafka Sink Configuration

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `brokers` | `[]string` | Yes | Producer bootstrap servers. |
| `topic` | `string` | Yes (if template disabled) | Static output topic. |
| `topic_template` | `string` | No | Future feature for templated routing. |
| `required_acks` | `int` (`0`,`1`,`-1`) | No | Sarama required acknowledgements (default: local). |
| `linger_ms` | `int` | No | Planned for batching. |
| `batch_bytes` | `int` | No | Planned. |
| `max_inflight` | `int` | No | Cap on outstanding requests. |
| `compression` | `string` | No | Compression algorithm (pending). |
| `key_selector` | object | No | Choose key source; default uses frame key. |
| `headers_pass` / `headers_drop` | `[]string` | No | Header allow/deny lists (future). |
| `retry.*` | object | No | Retry configuration (planned). |
| `timeout_ms` | `int` | No | Publish timeout. |
| `dlq.*` | object | No | Dead-letter queue configuration (planned). |

## YAML Example – Static Topic

```yaml
sink_configs:
  kafka:
    brokers:
      - "kafka:29092"
    topic: "normalized-events"
    required_acks: -1
```

## YAML Example – With DLQ (future)

```yaml
sink_configs:
  kafka:
    brokers: ["kafka:29092"]
    topic: "main"
    dlq:
      enable: true
      topic: "main.dlq"
      include_payload: true
      include_error: true
```

## Behaviour Notes

- Headers in the frame are converted to Sarama record headers.
- Context cancellation while waiting for the producer queue aborts the publish.
- The sink acknowledges only on success; failures bubble up to the runner.

