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
  config: ./source.kafka.yaml
transformers:
  - name: enrich
    type: grpc
    address: dns:///transformer:50052
sinks:
  - kafka
sink_configs:
  kafka: ./sink.kafka.yaml
```

### Public source config (`source.kafka.yaml`)

This file carries the user-facing settings only:

```yaml
schema_version: v1
brokers:
  - "kafka:29092"
topics:
  - "input.events"
group_id: "quanta-consumer"
start_from: "newest"        # oldest|newest
version: "3.6.0"            # optional, defaults to Sarama max
commit_mode: "e2e"          # auto|e2e
tls_enabled: false
sasl_user: ""
sasl_pass: ""
sarama_verbose: false
```

### Tuning overrides (`source.kafka.tuning.yaml`)

Operational knobs and performance limits belong in an optional sibling file:

```yaml
tuning:
  inflight_bytes: 268435456   # 256 MiB
  inflight_msgs:  4096
  window_bits:    4096
  commit_interval: 5s
  commit_step:    128
```

If the tuning file is omitted the engine applies conservative defaults.

## Environment overrides

Two prefixes are recognised and mapped to their respective blocks:

| Prefix | Example | Description |
|--------|---------|-------------|
| `QUANTA_SOURCE__` | `QUANTA_SOURCE__BROKERS=kafka:29092` | Overrides public configuration values. Nested keys use `__` to denote dots (e.g. `QUANTA_SOURCE__SASL__USER`). |
| `QUANTA_TUNING__` | `QUANTA_TUNING__INFLIGHT_BYTES=536870912` | Overrides tuning-only values. |

Environment overrides are applied after file loading. CLI flags (when available) should follow the same precedence and never write back to disk.

## Validation and invariants

At startup the source driver validates both public and tuning configs:

- `commit_mode` must be `auto` or `e2e`.
- `start_from` must be `oldest` or `newest`.
- `window_bits ≥ 256`.
- `inflight_msgs > 0` and `inflight_msgs ≤ window_bits`.
- `inflight_bytes > 0`.
- `commit_interval > 0` and `commit_step > 0`.

Invalid combinations fail fast with a descriptive error so the engine never starts in an unsafe state.

## Immutability & reload

- Public pipeline changes (`pipeline.yml`, `source.kafka.yaml`) require a full restart. The runner and source caches schema/flow information on boot.
- Tuning overrides (`source.kafka.tuning.yaml`, `QUANTA_TUNING__*`) are read during startup today. Hot-reload support is planned and will resize backpressure semaphores and commit intervals without dropping claims.

## Defaults summary

| Field | Default |
|-------|---------|
| `inflight_bytes` | `256 MiB` |
| `inflight_msgs` | `window_bits` (4096) |
| `window_bits` | `4096` |
| `commit_interval` | `5s` |
| `commit_step` | `128` |
| `start_from` | `newest` |
| `commit_mode` | `auto` |

## Quick reference

- Keep public configs minimal and version-controlled.
- Treat tuning files as ops-only overrides; store alongside the public file for discoverability.
- Use environment variables for container-specific tweaks, keeping `QUANTA_SOURCE__*` and `QUANTA_TUNING__*` in separate blocks.
- Watch startup logs—they record the resolved configuration and flag any violations before the engine begins consuming.
