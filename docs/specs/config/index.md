# Configuration Reference

Overview of all Quanta configuration files and their purposes.

## Configuration Files

| File | Purpose |
|------|---------|
| [kafka-source.md](kafka-source.md) | Kafka consumer configuration |
| [kafka-sink.md](kafka-sink.md) | Kafka producer configuration |
| [s3-sink.md](s3-sink.md) | S3 batched upload configuration |
| [clickhouse-sink.md](clickhouse-sink.md) | ClickHouse OLAP sink configuration |
| [stdout-sink.md](stdout-sink.md) | Debug output configuration |

## Pipeline Configuration

The main pipeline file (`topology/pipeline.yml`) wires components together:

```yaml
schema_version: v1

source:
  kind: kafka
  driver: sarama
  config: kafka_source.yml

# Schema definitions for sinks (Parquet, ClickHouse)
schemas:
  - schemas/ai_events.schema.yaml

transformers:
  - name: uppercase
    type: grpc
    address: "transformer:50052"
    timeout_ms: 1000

sinks:
  - kafka
  - s3
  - clickhouse

sink_configs:
  kafka:
    brokers: ["kafka:29092"]
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
    brokers: ["kafka:29092"]
    topic: "quanta-dlq"
```

## Schema Files

Schema files define JSON-to-column mapping for structured sinks.

Location: `topology/schemas/`

Used by:

- S3 sink (Parquet format)
- ClickHouse sink

See [Sink Specification](../sink.md#schema-mapping) for schema file format.

## Environment Overrides

Override any configuration at runtime:

```bash
# Source
export QUANTA_SOURCE__BROKERS="kafka1:9092,kafka2:9092"

# Tuning
export QUANTA_TUNING__INFLIGHT_MSGS=8192

# Sink credentials (recommended)
export AWS_ACCESS_KEY_ID=...
export AWS_SECRET_ACCESS_KEY=...
export CLICKHOUSE_PASSWORD=...
```

## Precedence

1. **Defaults** (lowest)
2. **YAML files**
3. **Environment variables** (highest)
