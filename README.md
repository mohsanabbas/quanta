# Quanta

A high-performance Go streaming engine that processes events from Kafka through transformers and delivers results to multiple sinks with end-to-end delivery guarantees.

**Specifications & Architecture**: Full design documentation lives in the [Spec Book](docs/specs/SUMMARY.md).

## Features

- **End-to-End Semantics**: At-least-once delivery with configurable commit modes (auto or e2e)
- **Kafka Source & Sink**: Production-ready Kafka integration with Sarama driver
- **S3 Sink**: Batched uploads with JSONL or Parquet format, schema-driven column mapping
- **ClickHouse Sink**: Real-time OLAP analytics with native protocol, batch inserts, LZ4/ZSTD compression
- **Schema Mapping**: ODCS-aligned data contracts for structured output (Parquet, ClickHouse)
- **gRPC Transformers**: Unary RPC support with configurable retry policies
- **Three-Path Error Handling**: Plugin errors → transformer sink, NackAware → engine DLQ, DeadLetterFn for infra failures
- **Engine-Managed DLQ**: Configurable dead-letter queue for undeliverable frames
- **Backpressure Management**: Combined byte and message count limits prevent memory overflow
- **Sliding Window Checkpoints**: Efficient out-of-order acknowledgment handling

## Quick Start

### Docker (Recommended)

```bash
# Build binaries for your architecture
make build-linux ARCH=arm64   # or amd64 for Intel/AMD

# Start the stack (Kafka + ClickHouse + Engine + Transformer)
make docker-up ARCH=arm64

# Watch the logs
make docker-logs

# Check metrics
curl -sf http://localhost:9100/metrics | head

# Access UIs
open http://localhost:8080      # Kafka UI
open http://localhost:8082      # S3 Manager
open http://localhost:8123/play # ClickHouse Play

# Clean up
make docker-down
```

This starts:
- Kafka broker (KRaft mode)
- Kafka UI (browse topics, consumers, lag)
- LocalStack (local S3)
- S3 Manager UI
- ClickHouse server
- Uppercase transformer (example gRPC service)
- Quanta engine

### Local Development

```bash
# Terminal 1: Start transformer
go run ./examples/transformers/uppercase --listen=:50052

# Terminal 2: Start engine
go run ./cmd/engine

# Override pipeline config
QUANTA_PIPELINE_YML=/path/to/pipeline.yml go run ./cmd/engine
```

## Sinks

| Sink | Protocol | Format | Use Case |
|------|----------|--------|----------|
| **Kafka** | TCP | Binary | Event streaming, fan-out |
| **S3** | HTTPS | JSONL, Parquet | Data lake, analytics |
| **ClickHouse** | Native TCP | Columnar | Real-time OLAP |
| **Stdout** | - | Text | Debugging |

### S3 Sink

```yaml
sink_configs:
  s3:
    bucket: quanta-output
    region: us-east-1
    prefix: events/
    
    format: parquet                              # or jsonl
    schema_file: topology/schemas/ai_events.schema.yaml
    
    batch_size: 1000
    flush_interval: 10s
    
    auth_strategy: static                        # static, iam-role, env
    access_key_id: ${AWS_ACCESS_KEY_ID}
    secret_access_key: ${AWS_SECRET_ACCESS_KEY}
```

### ClickHouse Sink

```yaml
sink_configs:
  clickhouse:
    host: "clickhouse:9000"
    database: analytics
    table: ai_events
    schema_file: topology/schemas/ai_events.schema.yaml
    
    auth_strategy: native
    username: default
    password_env: CLICKHOUSE_PASSWORD           # Env var (secure)
    
    batch_size: 5000
    flush_interval: 5s
    compression: lz4
```

## Schema Mapping

Both S3 (Parquet) and ClickHouse use ODCS-aligned schema files:

```yaml
# topology/schemas/ai_events.schema.yaml
kind: Schema
apiVersion: v1
name: ai_events
domain: ai-platform
owner: platform-team

columns:
  - name: event_id
    path: context.event_contract_id    # Dot-notation JSON path
    type: string
    required: true
    
  - name: event_time
    path: context.created_at
    type: timestamp
    required: true
    
  - name: provider
    path: properties.provider
    type: string
    required: true
```

## Configuration

### Pipeline (`topology/pipeline.yml`)

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
    timeout_ms: 1000

sinks:
  - kafka
  - s3
  - clickhouse

sink_configs:
  kafka:
    brokers: ["localhost:9094"]
    topic: "output-events"
  s3:
    bucket: quanta-output
    format: parquet
    schema_file: topology/schemas/ai_events.schema.yaml
  clickhouse:
    host: "clickhouse:9000"
    database: analytics
    table: ai_events
    schema_file: topology/schemas/ai_events.schema.yaml

dlq:
  enabled: true
  sink: kafka
  config:
    brokers: ["localhost:9094"]
    topic: "quanta-dlq"
```

### Environment Overrides

```bash
export QUANTA_SOURCE__BROKERS="kafka1:9092,kafka2:9092"
export QUANTA_TUNING__INFLIGHT_MSGS=8192
export CLICKHOUSE_PASSWORD=secret
export AWS_ACCESS_KEY_ID=...
export AWS_SECRET_ACCESS_KEY=...
```

## Developer Commands

| Command | Description |
|---------|-------------|
| `make build` | Build all Go modules |
| `make build-linux ARCH=arm64` | Cross-compile for Linux |
| `make docker-up ARCH=arm64` | Build and start stack |
| `make docker-down` | Stop containers |
| `make docker-logs` | Follow logs |
| `make proto` | Regenerate protobuf stubs |
| `make test` | Run tests |
| `make lint` | Run linters |

## Project Structure

```
cmd/
  engine/              Engine entrypoint
internal/
  pipeline/            Pipeline compiler and runner
  config/              Configuration loaders
  engine/              Bootstrap logic
source/
  kafka/               Kafka source (Sarama)
sink/
  kafka/               Kafka sink (AsyncProducer)
  s3/                  S3 sink (JSONL, Parquet)
  clickhouse/          ClickHouse sink (native protocol)
  schema/              Schema mapping (ODCS-aligned)
  batch/               Generic batching with Flusher[T]
  stdout/              Debug sink
examples/
  transformers/        Sample gRPC transformers
docs/
  specs/               Technical specifications
  guides/              User guides
topology/              Pipeline & schema configs
```

## Documentation

### Specifications
- [Architecture Overview](docs/specs/architecture.md)
- [Sink Specification](docs/specs/sink.md)
- [S3 Sink Config](docs/specs/config/s3-sink.md)
- [ClickHouse Sink Config](docs/specs/config/clickhouse-sink.md)
- [E2E Semantics](docs/specs/e2e-semantics.md)

### Guides
- [Tuning Guide](docs/guides/TUNING_GUIDE.md)

## Commit Modes

| Mode | Behavior | Use Case |
|------|----------|----------|
| **Auto** | Commit on emit | High throughput |
| **E2E** | Commit after sink ack | At-least-once delivery |

## Author

[Mohsan](https://github.com/mohsanabbas) — Software Engineer

## License

Apache-2.0
