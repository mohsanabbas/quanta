# Kafka Source Configuration

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `brokers` | `[]string` | Yes | Bootstrap servers. |
| `group_id` | `string` | Yes (for consumer groups) | Consumer group identifier. |
| `topics` | `[]string` | One of `topics` or `pattern` required | Explicit topic list. |
| `pattern` | `string` | Alternative to `topics` | Regex subscription (planned). |
| `start_offset` | `string` enum (`earliest`, `latest`) | No | Initial offset strategy (current driver: `start_from`). |
| `fetch_max_bytes` | `int` | No | Max bytes per fetch. |
| `session_timeout_ms` | `int` | No | Consumer session timeout. |
| `rebalance_timeout_ms` | `int` | No | Rebalance timeout. |
| `max_inflight_fetches` | `int` | No | Bound on pending fetch requests. |
| `commit_strategy` | enum | No | `auto` (after transform) or `e2e` (after sink). |
| `commit_interval_ms` | `int` | No | Periodic commit interval in auto mode. |
| `e2e_mode.enabled` | `bool` | No | Enable end-to-end commit gating. |
| `e2e_mode.fail_on_transform_error` | `bool` | No | Future option; current behaviour retries then acknowledges. |
| `tls.*` | object | No | TLS configuration for brokers. |
| `sasl.*` | object | No | SASL credentials. |
| `consumer_rack` | `string` | No | Rack identifier for rack-aware balancing. |
| `max_poll_records` | `int` | No | Future enhancement. |
| `metrics.emit_headers` | `bool` | No | Expose headers as metrics labels. |

## YAML Example – Static Topics

```yaml
schema_version: v1
source:
  kind: kafka
  driver: sarama
  config: kafka_source.yml
```

`kafka_source.yml`

```yaml
schema_version: v1
brokers:
  - "kafka:29092"
topics:
  - "events"
group_id: "quanta-engine"
start_from: "oldest"
commit_mode: "e2e"
checkpoint:
  commit_interval: 5s
backpressure:
  capacity: 1000
  check_interval: 100ms
```

## JSON Example – Regex Topics

```json
{
  "schema_version": "v1",
  "brokers": ["kafka:29092"],
  "pattern": "^events\.prod\\..*",
  "group_id": "quanta-regex",
  "start_from": "newest",
  "commit_mode": "auto"
}
```

## E2E Mode Example (commit after sink success)

```yaml
brokers: ["kafka:29092"]
topics: ["input"]
group_id: "e2e"
commit_mode: "e2e"
backpressure:
  capacity: 500
checkpoint:
  commit_interval: 10s
```

