# Quanta Configuration Reference (v1)

This document describes the versioned YAML schemas for pipeline and Kafka source configurations.

## pipeline.yml (schema_version: v1)

Top-level fields:
- **schema_version**: string (required) — currently "v1"
- **source**: object — stream source configuration
  - **kind**: string — currently "kafka"
  - **driver**: string — kafka driver name, e.g., "sarama"
  - **config**: string — path to Kafka config YAML (relative paths resolved from pipeline YAML location)
- **transformers**: array — ordered list of transform stages (optional)
  - **name**: string — identifier passed as PluginId
  - **type**: string — e.g., "grpc"
  - **address**: string — gRPC endpoint (host:port). Use service names in Docker (e.g., "uppercase:50052")
  - **max_in_flight**: int — reserved for future streaming mode
  - **timeout_ms**: int — per-request deadline in milliseconds
  - **content_type**: string — informational
  - **retry_policy**: object
    - **attempts**: int — number of retries on error/timeout
    - **backoff_ms**: int — fixed backoff between retries in milliseconds
- **sinks**: array — sink names, e.g., ["kafka", "stdout"]
- **sink_configs**: object — per-sink configuration blocks
  - **kafka**: object — Kafka sink configuration
    - **brokers**: array of strings — Kafka broker addresses
    - **topic**: string — output topic name
    - **acks**: string — "none", "local", or "all"
    - **version**: string — Kafka protocol version
    - **timeout**: duration — producer timeout
    - **retry_max**: int — maximum retries
    - **retry_backoff_min**: duration — minimum retry backoff
    - **retry_backoff_max**: duration — maximum retry backoff
  - **stdout**: object — (uses debug config for now)
- **debug**: object — stdout sink and debugging controls
  - **per_frame_delay_ms**: int — simulate per-frame latency
  - **print_counter**: bool — print frame sequence numbers
  - **ack_batch_size**: int — batch size for acks (1 = immediate)
  - **ack_flush_ms**: int — time-based ack flush interval (0 = off)
  - **print_value**: bool — print frame values
  - **value_max_bytes**: int — max bytes to print from values

### Example (Host)

```yaml
schema_version: v1

source:
  kind: kafka
  driver: sarama
  config: kafka_source.yml

transformers:
  - name: uppercase
    type: grpc
    address: "localhost:50052"
    max_in_flight: 100
    timeout_ms: 1000
    content_type: "application/protobuf"
    retry_policy:
      attempts: 3
      backoff_ms: 200

sinks:
  - kafka

sink_configs:
  kafka:
    brokers: ["localhost:9094"]
    topic: "quanta-output"
    acks: "all"
    version: "3.6.0"

debug:
  per_frame_delay_ms: 0
  print_counter: true
  ack_batch_size: 1
  ack_flush_ms: 0
```

### Example (Docker)

```yaml
schema_version: v1

source:
  kind: kafka
  driver: sarama
  config: kafka_source.docker.yml  # Docker-specific config

transformers:
  - name: uppercase
    type: grpc
    address: "uppercase:50052"      # Service name in Docker network
    timeout_ms: 1000
    retry_policy:
      attempts: 3
      backoff_ms: 200

sinks:
  - kafka

sink_configs:
  kafka:
    brokers: ["kafka:29092"]        # Internal Docker network
    topic: "quanta-output"
    acks: "all"
```

---

## kafka_source.yml (schema_version: v1)

Main Kafka source configuration with functional settings:

- **schema_version**: string (required) — currently "v1"
- **brokers**: array of strings (required) — Kafka broker addresses
- **topics**: array of strings (required) — topics to consume from
- **group_id**: string (required) — consumer group ID
- **start_from**: string — "oldest" (from beginning) or "newest" (only new messages)
- **version**: string — Kafka protocol version (e.g., "3.6.0")
- **commit_mode**: string — "auto" or "e2e"
  - **auto**: Offsets marked immediately after emit (fire-and-forget, high throughput)
  - **e2e**: Offsets committed after sink ack (at-least-once delivery)
- **backpressure_strategy**: string — "combined", "count", or "size"
  - **combined**: Enforces both byte and message limits (default)
  - **count**: Only message count limits
  - **size**: Only byte size limits
- **checkpoint_strategy**: string — "sliding_window" (only option currently)
- **commit_strategy_type**: string — "hybrid", "periodic", or "immediate"
  - **hybrid**: Commits on base advance + time-based (default, best balance)
  - **periodic**: Only time-based commits
  - **immediate**: Commits on every base advance (highest safety)
- **tls_enabled**: bool — enable TLS encryption
- **sasl_user**: string — SASL username (optional)
- **sasl_pass**: string — SASL password (optional)
- **sarama_verbose**: bool — enable verbose Sarama logging for debugging

### Example

```yaml
schema_version: v1
brokers: ["localhost:9094"]
topics: ["input-topic"]
group_id: "quanta-consumer"
start_from: "newest"
version: "3.6.0"
commit_mode: "e2e"
backpressure_strategy: "combined"
checkpoint_strategy: "sliding_window"
commit_strategy_type: "hybrid"
tls_enabled: false
sasl_user: ""
sasl_pass: ""
sarama_verbose: false
```

---

## kafka_source.tuning.yml

**Automatically loaded** by inserting `.tuning` before the file extension. This file is optional; if missing, defaults are applied.

Performance and operational tuning parameters:

- **inflight_bytes**: int — maximum bytes held in memory before backpressure blocks (e.g., 268435456 for 256 MiB)
- **inflight_msgs**: int — maximum concurrent unacknowledged messages
- **window_bits**: int — checkpoint window size (must be ≥ inflight_msgs, recommended 2×)
- **commit_interval**: duration — time between periodic commits (e.g., "5s")
- **commit_step**: int — minimum offset advance to trigger commit (e.g., 500)

### File Naming Convention

The tuning file is auto-discovered:
- `kafka_source.yml` → `kafka_source.tuning.yml`
- `kafka_source.docker.yml` → `kafka_source.docker.tuning.yml`
- `config/prod.yaml` → `config/prod.tuning.yaml`

### Example

```yaml
inflight_bytes: 268435456   # 256 MiB
inflight_msgs: 4096         # Concurrent messages
window_bits: 8192           # 2× inflight_msgs for out-of-order acks
commit_interval: 5s         # Commit at least every 5 seconds
commit_step: 500            # Commit when base advances by 500 offsets
```

### Defaults

| Parameter | Default | Notes |
|-----------|---------|-------|
| inflight_bytes | 256 MiB | Combined backpressure only |
| inflight_msgs | 4096 | All backpressure strategies |
| window_bits | 4096 | Must be ≥ inflight_msgs |
| commit_interval | 5s | Periodic and hybrid strategies |
| commit_step | 500 | Hybrid strategy only |

---

## Environment Variable Overrides

Override configuration at runtime without editing files:

### Source Configuration

Prefix: `QUANTA_SOURCE__`

Examples:
```bash
export QUANTA_SOURCE__BROKERS="kafka1:9092,kafka2:9092"
export QUANTA_SOURCE__GROUP_ID="my-consumer"
export QUANTA_SOURCE__COMMIT_MODE="e2e"
export QUANTA_SOURCE__SARAMA_VERBOSE="true"
```

### Tuning Parameters

Prefix: `QUANTA_TUNING__`

Examples:
```bash
export QUANTA_TUNING__INFLIGHT_MSGS=8192
export QUANTA_TUNING__COMMIT_INTERVAL=10s
export QUANTA_TUNING__WINDOW_BITS=16384
```

### Precedence

1. **Defaults** (lowest)
2. **YAML files** (main + tuning)
3. **Environment variables** (highest)

---

## Docker Volumes

For Docker deployments, mount both main and tuning configs:

```yaml
# docker-compose.yml
volumes:
  - ./kafka_source.docker.yml:/config/kafka_source.docker.yml:ro
  - ./kafka_source.docker.tuning.yml:/config/kafka_source.docker.tuning.yml:ro
  - ./pipeline.docker.yml:/config/pipeline.docker.yml:ro
```

---

## Additional Resources

- [docs/specs/configuration.md](docs/specs/configuration.md) - Configuration specification
- [docs/guides/TUNING_GUIDE.md](docs/guides/TUNING_GUIDE.md) - Performance tuning guide
- [docs/guides/TUNING_LOADING_FLOW.md](docs/guides/TUNING_LOADING_FLOW.md) - How configs are loaded
