# Quanta

Quanta is a Go streaming engine that takes events from Kafka, runs them through a chain of processors, and delivers the results to sinks (Kafka, stdout, and more to come). The runtime keeps context and backpressure information from the source all the way to the sink so we can offer end-to-end commit semantics.

**Specifications & Architecture**: the full design notes live in the [Spec Book](docs/specs/SUMMARY.md).

## What you get today

- **Kafka `Source` --> Transformer --> Sink** pipeline with at-least-once delivery and optional end-to-end commits.
- **gRPC transformers** with unary RPC today and a clear path to streaming RPCs tomorrow.
- **Adapter registry** for sources and sinks—drop in your own driver via `init` registration.
- **Context-aware pipeline** so shutdowns and timeouts behave predictably.

## Try it out locally

### 1. Run the demo transformer

```bash
go run ./examples/transformers/uppercase --listen=:50052
```

### 2. Run the engine

```bash
go run ./cmd/engine
```

By default the engine loads `pipeline.yml`, reads from Kafka, calls the transformer above, and prints results to stdout. Override the pipeline with `QUANTA_PIPELINE_YML=/path/to/pipeline.yml`.

### 3. Check it’s working

- The transformer should log `card-registration-normalizer listening on :50052` (or similar).
- The engine logs frame counters and ack commits; if you have Kafka UI, you should see offsets advance.

## Docker workflow

Requirements: Docker Desktop / Compose v2 and a reachable Kafka broker (`host.docker.internal:9094` on macOS/Windows).

```bash
# build linux binaries for your arch
make build-linux ARCH=arm64   # or amd64

# launch the stack (engine + transformer)
make docker-up ARCH=arm64

# tail logs / check metrics
make docker-logs
curl -sf http://localhost:9100/metrics | head

# tear down when done
make docker-down
```

## Developer toolbox

| Command | Description |
|---------|-------------|
| `make build` | Go build all modules. |
| `make proto` | Regenerate protobuf stubs. |
| `make build-linux ARCH=...` | Cross-compile static binaries for Docker. |
| `make docker-build ARCH=...` | Build engine and transformer images. |
| `make docker-up ARCH=...` | Rebuild images and start the Compose stack. |
| `make docker-logs` | Follow container logs. |
| `make docker-smoke` | Quick health probe of the metrics endpoint. |

## Troubleshooting quick hits

- **Architecture mismatch**: build binaries for the same arch as the container (`make build-linux ARCH=arm64`).
- **Kafka not reachable**: check broker address inside containers; macOS/Windows usually need `host.docker.internal`.
- **Processing stalls**: look at transformer logs for retries. Increase `backpressure.capacity` if you expect larger bursts.
- **Port conflicts**: adjust `--listen` for transformers or the metrics port in `pipeline.yml`.

## Project layout

```
cmd/engine                Engine entrypoint
internal/pipeline         Compiler + runner wiring sources --> transformers --> sinks
source/kafka              Sarama-based source driver, backpressure & checkpoints
sink/kafka                Kafka sink adapter
sink/stdout               Debug sink with optional batching
examples/transformers     Sample transformer implementations
docs/specs                Specification book (architecture, configs, semantics)
```

## License

Apache-2.0 
