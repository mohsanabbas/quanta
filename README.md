# Quanta

A high-performance Go streaming engine that processes events from Kafka through transformers and delivers results to sinks with end-to-end delivery guarantees.

**Specifications & Architecture**: Full design documentation lives in the [Spec Book](docs/specs/SUMMARY.md).

## Features

- **End-to-End Semantics**: At-least-once delivery with configurable commit modes (auto or e2e)
- **Kafka Source & Sink**: Production-ready Kafka integration with Sarama driver
- **gRPC Transformers**: Unary RPC support with configurable retry policies
- **Backpressure Management**: Combined byte and message count limits prevent memory overflow
- **Context-Aware Pipeline**: Graceful shutdowns and timeout propagation throughout the stack
- **Sliding Window Checkpoints**: Efficient out-of-order acknowledgment handling
- **Flexible Configuration**: Separate public config from performance tuning

## Quick Start

### Docker (Recommended)

The fastest way to try Quanta with a complete Kafka stack:

```bash
# Build binaries for your architecture
make build-linux ARCH=arm64   # or amd64 for Intel/AMD

# Start the stack (Kafka + Engine + Transformer)
make docker-up ARCH=arm64

# Watch the logs
make docker-logs

# Check metrics
curl -sf http://localhost:9100/metrics | head

# Access Kafka UI
open http://localhost:8080

# Clean up
make docker-down
```

This starts:
- Kafka broker (Bitnami Kafka with KRaft)
- Kafka UI (browse topics, consumers, lag)
- Uppercase transformer (example gRPC service)
- Quanta engine (processes events)

### Local Development

Run components separately on your host:

```bash
# Terminal 1: Start the transformer
go run ./examples/transformers/uppercase --listen=:50052

# Terminal 2: Start the engine
go run ./cmd/engine

# Override pipeline config if needed
QUANTA_PIPELINE_YML=/path/to/pipeline.yml go run ./cmd/engine
```

**Note**: For local runs, ensure Kafka is accessible at `localhost:9094` and update `pipeline.yml` accordingly.

## Configuration

Quanta uses a two-tier configuration approach:

### 1. Pipeline Configuration (`pipeline.yml`)

Defines the data flow and component wiring:

```yaml
schema_version: v1

source:
  kind: kafka
  driver: sarama
  config: kafka_source.yml        # Main Kafka config

transformers:
  - name: uppercase
    type: grpc
    address: "localhost:50052"    # Use service name in Docker
    timeout_ms: 1000
    retry_policy:
      attempts: 3
      backoff_ms: 200

sinks:
  - kafka                          # or stdout for debugging

sink_configs:
  kafka:
    brokers: ["localhost:9094"]
    topic: "quanta-output"
    acks: "all"
```

### 2. Kafka Source Configuration

**Main Config** (`kafka_source.yml`):
```yaml
schema_version: v1
brokers: ["localhost:9094"]
topics: ["input-topic"]
group_id: "quanta-consumer"
start_from: "newest"              # oldest|newest
commit_mode: "e2e"                # auto|e2e
version: "3.6.0"
```

**Tuning Config** (`kafka_source.tuning.yml` - optional):
```yaml
inflight_bytes: 268435456         # 256 MiB
inflight_msgs: 4096               # Concurrent messages
window_bits: 8192                 # Checkpoint window (≥ inflight_msgs)
commit_interval: 5s               # Time-based commits
commit_step: 500                  # Offset-based commits
```

The tuning file is **automatically loaded** by inserting `.tuning` before the extension.

### Environment Overrides

Override configuration at runtime without editing files:

```bash
# Source configuration
export QUANTA_SOURCE__BROKERS="kafka1:9092,kafka2:9092"
export QUANTA_SOURCE__GROUP_ID="my-consumer"

# Tuning parameters
export QUANTA_TUNING__INFLIGHT_MSGS=8192
export QUANTA_TUNING__COMMIT_INTERVAL=10s

docker-compose up -d
```

### Configuration Precedence

1. **Defaults** (lowest priority)
2. **YAML files** (main + tuning)
3. **Environment variables** (highest priority)

## Developer Commands

| Command                        | Description                         |
|--------------------------------|-------------------------------------|
| `make build`                   | Build all Go modules for current OS |
| `make build-linux ARCH=arm64`  | Cross-compile for Linux (Docker)    |
| `make docker-build ARCH=arm64` | Build Docker images                 |
| `make docker-up ARCH=arm64`    | Rebuild and start stack             |
| `make docker-down`             | Stop and remove containers          |
| `make docker-logs`             | Follow all container logs           |
| `make docker-smoke`            | Health check (metrics endpoint)     |
| `make proto`                   | Regenerate protobuf stubs           |
| `make test`                    | Run all tests                       |
| `make lint`                    | Run linters                         |

## Documentation

### Specifications
- [Architecture Overview](docs/specs/architecture.md)
- [Configuration Management](docs/specs/configuration.md)
- [Source Specification](docs/specs/source.md)
- [Sink Specification](docs/specs/sink.md)
- [E2E Semantics](docs/specs/e2e-semantics.md)
- [Error Handling](docs/specs/error-handling.md)

### Guides
- [Tuning Guide](docs/guides/TUNING_GUIDE.md) - Performance tuning and scenarios
- [Bug Fixes](docs/guides/BUGFIXES.md) - Recent fixes and improvements
- [Tuning Loading Flow](docs/guides/TUNING_LOADING_FLOW.md) - How configuration is loaded

### Configuration Reference
- [CONFIGS.md](CONFIGS.md) - Complete YAML schema reference

## Troubleshooting

### Common Issues

**Architecture Mismatch**
```bash
# Ensure binaries match container architecture
make build-linux ARCH=arm64  # For Apple Silicon
make build-linux ARCH=amd64  # For Intel/AMD
```

**Kafka Connection Issues**
- Docker: Use service names (`kafka:29092`)
- Host: Use `localhost:9094` or `host.docker.internal:9094`
- Check `docker-compose logs kafka` for broker logs

**Processing Stalls / Stuck Partitions**
```yaml
# Increase commit frequency in kafka_source.tuning.yml
commit_interval: 2s      # Down from 5s
commit_step: 100         # Down from 500
```

**High Memory Usage**
```yaml
# Reduce in-flight limits in kafka_source.tuning.yml
inflight_bytes: 134217728  # 128 MiB
inflight_msgs: 2000
```

**Shutdown Panic (Fixed)**
- Recent fix addresses semaphore release issues
- Update to latest version if experiencing crashes on Ctrl+C

### Debugging

```bash
# Enable verbose Sarama logging
# In kafka_source.yml:
sarama_verbose: true

# Use stdout sink for debugging
# In pipeline.yml:
sinks:
  - stdout

# Check consumer lag in Kafka UI
open http://localhost:8080
```

## Project Structure

```
cmd/
  engine/              Engine entrypoint
  ctl/                 CLI tools (future)
internal/
  pipeline/            Pipeline compiler and runner
  config/              Configuration loaders
  engine/              Bootstrap logic
source/
  kafka/               Kafka source driver (Sarama)
sink/
  kafka/               Kafka sink
  stdout/              Debug sink
examples/
  transformers/        Sample gRPC transformers
docs/
  specs/               Technical specifications
  guides/              User guides and tutorials
```

## Commit Modes

### Auto Mode (High Throughput)
- Offsets marked immediately after emit
- Fast processing, fire-and-forget
- ⚠️ Some message loss possible on crash
- Use when: Speed > safety

### E2E Mode (At-Least-Once)
- Offsets committed after sink acknowledgment
- Guaranteed delivery
- Handles out-of-order acks with sliding window
- Use when: No message loss tolerable

See [docs/guides/TUNING_GUIDE.md](docs/guides/TUNING_GUIDE.md) for detailed scenarios and tuning advice.

## Contributing

Contributions welcome! See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## License

Apache-2.0
