# Quanta config reference (v1)

This doc describes the versioned YAML schemas for pipeline and Kafka source configs and how to run locally or with Docker.

## pipeline.yml (schema_version: v1)

Top-level fields:
- schema_version: string (required) — currently "v1".
- source: object — stream source.
  - kind: string — currently "kafka".
  - driver: string — kafka driver, e.g. "sarama".
  - config: string — path to Kafka config YAML. Relative paths are resolved relative to the pipeline YAML location.
- transformers: array — ordered list of transform stages (optional).
  - name: string — identifier passed as PluginId.
  - type: string — e.g. "grpc".
  - address: string — gRPC endpoint (host:port). In Docker use service name (e.g. "uppercase:50052").
  - max_in_flight: int — reserved for future streaming mode.
  - timeout_ms: int — per-request deadline.
  - content_type: string — informational.
  - retry_policy:
    - attempts: int — transformer retries on error/timeout.
    - backoff_ms: int — fixed backoff between retries.
- sinks: array — sink names, e.g. ["stdout"].
- sink_configs: object — per-sink config blocks (future; stdout uses Debug for now).
- debug: object — stdout sink demo controls.
  - per_frame_delay_ms: int — simulate per-frame latency.
  - print_counter: bool — print sequence.
  - ack_batch_size: int — batch size for acks (1 = immediate).
  - ack_flush_ms: int — time-based ack flush (0 = off).

Example (host):

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
    content_type: application/protobuf
    retry_policy: { attempts: 3, backoff_ms: 200 }
sinks: [stdout]
debug: { per_frame_delay_ms: 0, print_counter: true, ack_batch_size: 1, ack_flush_ms: 0 }
```

Docker variant uses address: "uppercase:50052" and config: kafka_source.docker.yml.

## kafka_source.yml (schema_version: v1)

- schema_version: string (required) — currently "v1".
- brokers: [string] — bootstrap server(s).
- topics: [string] — topics to subscribe.
- group_id: string — consumer group.
- start_from: string — "oldest" | "newest" (default newest).
- version: string — Kafka broker version (e.g. "3.6.0").
- tls_enabled: bool
- sasl_user: string
- sasl_pass: string
- commit_mode: string — "auto" or "e2e".
- backpressure:
  - capacity: int — max in-flight frames.
  - check_interval: duration — token refill tick.
- checkpoint:
  - commit_interval: duration — commit throttle.

Example (host):

```yaml
schema_version: v1
brokers: ["localhost:9094"]
topics: ["event-tracking_track-events-approved"]
group_id: "quanta-src-track-events-approved"
start_from: "oldest"
version: "3.6.0"
commit_mode: "e2e"
backpressure: { capacity: 1000, check_interval: 500ms }
checkpoint: { commit_interval: 1s }
tls_enabled: false
sasl_user: ""
sasl_pass: ""
```

Docker variant uses brokers: ["host.docker.internal:9094"] so the container can reach the host Kafka on macOS/Windows.

## Behavior notes

- Pipeline compiler enforces schema_version=v1 and resolves source.config relative to the pipeline file location (works with mounted configs in Docker).
- Kafka config loader enforces schema_version=v1.
- E2E semantics: the source holds a backpressure token until a sink (or transformer drop) acks the record; commits are throttled by checkpoint.commit_interval.
- Transformer retries: errors/timeouts are retried attempts times with backoff; after that, the engine drops+acks to avoid deadlocks.

## Run locally (host)

Terminal 1:
```bash
go run ./examples/transformers/uppercase --listen=:50052
```

Terminal 2:
```bash
go run ./cmd/engine
```

You should see transformer logs ("received event …") and sink offsets. Kafka UI lag updates every commit_interval.

## Run with Docker

Prereqs: Docker Desktop with Compose v2, a Kafka broker on the host at localhost:9094 (macOS/Windows containers use host.docker.internal:9094).

Build host Linux binaries and images, start stack:
```bash
make docker-up
make docker-logs   # follow logs
make docker-smoke  # quick metrics check
```

Stop:
```bash
make docker-down
```

