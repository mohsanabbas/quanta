# Configuration & Precedence

Quanta separates functional pipeline configuration from operational tuning. This document describes the files, environment variables, and runtime guarantees that apply to the source stack.

## Files and schema

### Pipeline YAML (`pipeline.yml`)

The pipeline spec declares the logical data flow:

```yaml
schema_version: v1
source:
  kind: kafka
  driver: sarama
  config: kafka_source.yml        # Main config path

transformers:
  - name: enrich
    type: grpc
    address: "localhost:50052"    # Use service name in Docker
    timeout_ms: 1000
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
```

### Public source config (`kafka_source.yml`)

This file carries the user-facing settings only:

```yaml
schema_version: v1
brokers:
  - "kafka:29092"
topics:
  - "input-topic"
group_id: "quanta-consumer"
start_from: "newest"              # oldest|newest
version: "3.6.0"                  # optional, defaults to Sarama max
commit_mode: "e2e"                # auto|e2e
backpressure_strategy: "combined" # combined|count|size
checkpoint_strategy: "sliding_window"
commit_strategy_type: "hybrid"    # hybrid|periodic|immediate
tls_enabled: false
sasl_user: ""
sasl_pass: ""
sarama_verbose: false
```

### Tuning overrides (`kafka_source.tuning.yml`)

Operational knobs and performance limits belong in an optional sibling file that is **automatically loaded** by inserting `.tuning` before the file extension:

```yaml
inflight_bytes: 268435456   # 256 MiB
inflight_msgs: 4096         # Concurrent messages
window_bits: 8192           # Checkpoint window (≥ inflight_msgs)
commit_interval: 5s         # Time-based commits
commit_step: 500            # Offset-based commits
```

**Automatic Loading**: If your main config is `kafka_source.docker.yml`, the system automatically looks for and loads `kafka_source.docker.tuning.yml`. If the tuning file doesn't exist, defaults are applied.

**File Naming Examples**:
- `kafka_source.yml` → `kafka_source.tuning.yml`
- `kafka_source.docker.yml` → `kafka_source.docker.tuning.yml`
- `config/prod.yaml` → `config/prod.tuning.yaml`

## Environment overrides

Two prefixes are recognised and mapped to their respective blocks:

| Prefix            | Example                                   | Description                                                                 |
|-------------------|-------------------------------------------|-----------------------------------------------------------------------------|
| `QUANTA_SOURCE__` | `QUANTA_SOURCE__BROKERS=kafka:29092`      | Overrides public configuration values. Nested keys use `__` to denote dots. |
| `QUANTA_TUNING__` | `QUANTA_TUNING__INFLIGHT_BYTES=536870912` | Overrides tuning-only values.                                               |

Environment overrides are applied after file loading and have the highest precedence.

## Configuration Precedence

1. **Defaults** (the lowest priority)
2. **YAML files** (main config + tuning config)
3. **Environment variables** (the highest priority)

## Validation and invariants

At startup the source driver validates both public and tuning configs:

- `commit_mode` must be `auto` or `e2e`.
- `start_from` must be `oldest` or `newest`.
- `backpressure_strategy` must be `combined`, `count`, or `size`.
- `checkpoint_strategy` must be `sliding_window`.
- `commit_strategy_type` must be `hybrid`, `periodic`, or `immediate`.
- `window_bits ≥ 256`.
- `inflight_msgs > 0` and `inflight_msgs ≤ window_bits`.
- `inflight_bytes > 0`.
- `commit_interval > 0` and `commit_step > 0`.

Invalid combinations fail fast with a descriptive error so the engine never starts in an unsafe state.

## Immutability & reload

- Public pipeline changes (`pipeline.yml`, `kafka_source.yml`) require a full restart. The runner and source caches schema/flow information on boot.
- Tuning overrides (`kafka_source.tuning.yml`, `QUANTA_TUNING__*`) are read during startup. Hot-reload support is planned and will resize backpressure semaphores and commit intervals without dropping claims.

## Defaults summary

| Field                   | Default          | Notes                                   |
|-------------------------|------------------|-----------------------------------------|
| `inflight_bytes`        | `256 MiB`        | Combined backpressure only              |
| `inflight_msgs`         | `4096`           | All backpressure strategies             |
| `window_bits`           | `4096`           | Must be ≥ inflight_msgs; recommended 2× |
| `commit_interval`       | `5s`             | Periodic and hybrid strategies          |
| `commit_step`           | `500`            | Hybrid strategy only                    |
| `start_from`            | `newest`         |                                         |
| `commit_mode`           | `auto`           | Use `e2e` for at-least-once             |
| `backpressure_strategy` | `combined`       |                                         |
| `checkpoint_strategy`   | `sliding_window` |                                         |
| `commit_strategy_type`  | `hybrid`         |                                         |

## Commit Modes

### Auto Mode
- Offsets marked immediately after emit to pipeline
- Backpressure tokens are released immediately
- Sarama commits periodically in the background
- ⚠️ Some message loss possible on a crash
- ✅ Maximum throughput

### E2E Mode
- Offsets committed after sink acknowledgment
- Backpressure tokens held until ack
- Checkpoint manager tracks all in-flight messages
- ✅ At-least-once delivery
- ⚠️ Higher memory usage

See [../guides/TUNING_GUIDE.md](../guides/TUNING_GUIDE.md) for detailed tuning scenarios.
