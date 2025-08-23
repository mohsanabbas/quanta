# Quanta

Early-stage streaming/event-processing engine in Go. Reads from Kafka, invokes transformer plugins, and emits to sinks. Exposes gRPC control and a metrics endpoint.

Status: source→transformers→sinks path is working with E2E acks; example gRPC transformer included.

## Features
- Kafka source (Sarama) with backpressure and E2E commit support.
- Pluggable transformers over gRPC; retry/backoff and drop+ack on exhaustion.
- Stdout sink with configurable ack batching.
- Versioned YAML configs (schema_version: v1).
- Docker images from host-built Linux binaries (arm64/amd64).

## Configs (v1)
- See CONFIGS.md for the full schema and examples.
- pipeline.yml points to kafka_source.yml; relative paths are resolved relative to the pipeline file.

## Quick start (host)
1) Start the example transformer:
```bash
go run ./examples/transformers/uppercase --listen=:50052
```
2) Start the engine (uses pipeline.yml by default):
```bash
go run ./cmd/engine
```
3) Validate:
- Transformer logs should print: `uppercase plugin listening on :50052` and `received event ...` lines.
- Engine prints sink offsets; Kafka UI shows periodic lag commits (E2E mode).

Tip: Override pipeline path with `QUANTA_PIPELINE_YML=/abs/path/pipeline.yml`.

## Quick start (Docker)
Prereqs
- Docker Desktop (Compose v2).
- Kafka broker reachable at:
  - macOS/Windows: `host.docker.internal:9094` (default in kafka_source.docker.yml)
  - Linux: set `brokers: ["<host-ip>:9094"]` or run Kafka in Compose and update brokers accordingly.

Build and run (arm64 default; use `ARCH=amd64` on Intel/AMD hosts):
```bash
make docker-up ARCH=arm64     # or ARCH=amd64
make docker-logs              # follow logs
make docker-smoke             # quick metrics probe
```
Validate
- Uppercase logs: `uppercase plugin listening on :50052`.
- Metrics endpoint served:
```bash
curl -sf http://localhost:9100/metrics | head
```

Stop
```bash
make docker-down
```

## Makefile targets
- `make build` — build all packages.
- `make proto` — regenerate protobuf stubs.
- `make build-linux ARCH=arm64|amd64` — cross-compile static Linux binaries into `bin/linux-<arch>/`.
- `make docker-build ARCH=...` — build `quanta-engine:local` and `quanta-uppercase:local` using host-built binaries.
- `make docker-up ARCH=...` — rebuild images and start Compose stack.
- `make docker-logs`, `make docker-down`, `make docker-smoke` — utility targets.

## Validation results
- Local build: PASS.
- Docker (first attempt): FAIL due to arch mismatch (`taggedPointerPack` runtime error) when copying amd64 binaries into an arm64 runtime.
- Fix: parameterized Dockerfiles with `BIN_DIR`, Makefile with `ARCH`, and Compose build args; rebuilt arm64 binaries/images.
- Docker (arm64): PASS — both containers start; transformer logs `listening`; `curl http://localhost:9100/metrics` returns metrics.

## Troubleshooting
- Arch mismatch errors (e.g., `taggedPointerPack`): build with the correct `ARCH` and ensure Compose builds with `BIN_DIR=bin/linux-<arch>`.
- Kafka connectivity: make sure the broker is reachable from containers (use `host.docker.internal` on macOS/Windows or host IP on Linux).
- E2E mode appears stalled: transformer errors/timeouts are retried; after retries, engine drops+acks to avoid deadlocks. Check transformer logs; consider increasing `backpressure.capacity`.
- Port conflicts: change transformer listen port or engine metrics port mappings in Compose.

## Layout
- cmd/engine — engine binary (reads `QUANTA_PIPELINE_YML` or `pipeline.yml`).
- internal/pipeline — compiler and runner; wires source→transformers→sinks.
- source/kafka — Sarama driver, backpressure, checkpoint manager, config.
- internal/transform — plugin client (gRPC/in-process shim).
- examples/transformers/uppercase — example gRPC transformer.
- sink/stdout — stdout sink with ack batching.

## License
Apache-2.0 (planned).
