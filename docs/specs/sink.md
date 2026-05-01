# Sink Specification

Sinks consume processed frames and deliver them to external systems with delivery guarantees.

## Interface

```go
type Adapter interface {
    Name() string
    Caps() Capabilities
    Publish(ctx context.Context, frame *pb.Frame) error
    Close(ctx context.Context) error
}

type Capabilities struct {
    AckAware  bool  // Async acknowledgment
    NackAware bool  // Delivery failure detection
}
```

## Available Sinks

| Sink | Protocol | AckAware | NackAware | Batching | Use Case |
|------|----------|----------|-----------|----------|----------|
| **kafka** | TCP | ✓ | ✓ | Producer batching | Event streaming, fan-out |
| **s3** | HTTPS | ✓ | ✓ | File batching | Data lake, archival |
| **clickhouse** | TCP (native) | ✓ | ✓ | Batch inserts | OLAP analytics |
| **stdout** | - | - | - | - | Debugging |

---

## Kafka Sink

Async producer using Sarama `AsyncProducer`.

### Configuration

```yaml
sink_configs:
  kafka:
    brokers: ["kafka:29092"]
    topic: "output-events"
    required_acks: -1        # all replicas (default: 1)
    compression: snappy      # none, gzip, snappy, lz4, zstd
    max_message_bytes: 1048576
```

### Behavior
- Non-blocking `Publish` → enqueues to producer
- Ack loop reads `Successes` and `Errors` channels
- Frame key becomes Kafka message key
- Headers preserved from frame

---

## S3 Sink

Batched uploads to S3-compatible storage.

### Configuration

```yaml
sink_configs:
  s3:
    bucket: quanta-output
    region: us-east-1
    prefix: events/
    
    # Format: jsonl (default) or parquet
    format: parquet
    schema_file: topology/schemas/ai_events.schema.yaml
    
    # Batching
    batch_size: 1000
    flush_interval: 10s
    
    # Auth: static, iam-role, env
    auth_strategy: static
    access_key_id: ${AWS_ACCESS_KEY_ID}
    secret_access_key: ${AWS_SECRET_ACCESS_KEY}
    
    # Optional
    endpoint: http://localhost:4566  # LocalStack
    path_style: true
    compression: snappy              # For parquet: snappy (default)
```

### Formats

| Format | Content-Type | Schema Required | Use Case |
|--------|--------------|-----------------|----------|
| `jsonl` | `application/x-ndjson` | No | Simple, human-readable |
| `parquet` | `application/vnd.apache.parquet` | Yes | Analytics (Spark, Athena, BigQuery) |

### Parquet Features
- Snappy compression (Spark/Databricks compatible)
- Schema-driven column mapping via `sink/schema`
- Microsecond timestamp precision
- DataPageVersion 2 for better encoding

### Output Structure
```
s3://quanta-output/
└── events/
    ├── 2025-05-01T10-00-00Z_batch001.parquet
    ├── 2025-05-01T10-00-05Z_batch002.parquet
    └── ...
```

---

## ClickHouse Sink

Real-time OLAP analytics via native protocol.

### Configuration

```yaml
sink_configs:
  clickhouse:
    # Connection (host OR hosts for cluster)
    host: "clickhouse:9000"
    # hosts: ["ch1:9000", "ch2:9000"]  # Cluster mode
    database: analytics
    table: ai_events
    
    # Schema mapping
    schema_file: topology/schemas/ai_events.schema.yaml
    
    # Auth: native, tls, env
    auth_strategy: native
    username: default
    password_env: CLICKHOUSE_PASSWORD  # Env var (recommended)
    
    # Batching
    batch_size: 5000
    flush_interval: 5s
    
    # Compression: none, lz4 (default), zstd
    compression: lz4
    
    # TLS (production)
    tls: true
    ca_cert: /etc/ssl/ca.pem
    # client_cert: /etc/ssl/client.pem  # mTLS
    # client_key: /etc/ssl/client.key
    
    # Connection pool
    max_idle_conns: 5
    max_open_conns: 10
    conn_max_lifetime: 1h
```

### Authentication Strategies

| Strategy | Use Case           | Credentials                                                                                     |
|----------|--------------------|-------------------------------------------------------------------------------------------------|
| `native` | Simple deployments | `username` + `password` or `password_env`                                                       |
| `tls`    | Production mTLS    | `client_cert` + `client_key` + `ca_cert`                                                        |
| `env`    | K8s secrets        | `CLICKHOUSE_USER`, `CLICKHOUSE_PASSWORD` env vars (override with `username_env`/`password_env`) |

### Security
- **Never logs**: hosts, username, password, certificates
- Env var precedence: `password_env` overrides `password`
- TLS validation: `tls_insecure: true` for dev only

### Batch Insert Flow
```
Frame → Flusher.Add() → batch fills → Seal
                                        ↓
                              conn.PrepareBatch(INSERT...)
                              batch.Append(row...)
                              batch.Send() → Ack/Nack
```

### ClickHouse Table Example
```sql
CREATE TABLE analytics.ai_events (
    event_id String,
    event_type LowCardinality(String),
    event_time DateTime64(6, 'UTC'),
    provider LowCardinality(String),
    model String,
    input_tokens Int64,
    output_tokens Int64,
    latency_ms Int64,
    status LowCardinality(String)
) ENGINE = MergeTree()
ORDER BY (event_time, event_id)
PARTITION BY toYYYYMM(event_time);
```

---

## Stdout Sink

Debug sink for development.

### Configuration

```yaml
sink_configs:
  stdout:
    print_counter: true
    print_value: true
    value_max_bytes: 256
```

---

## Schema Mapping

Both S3 (Parquet) and ClickHouse sinks use `sink/schema` for JSON-to-column mapping.

### Schema File (ODCS-aligned)

```yaml
kind: Schema
apiVersion: v1
name: ai_events
domain: ai-platform
owner: platform-team

columns:
  - name: event_id
    path: context.event_contract_id  # Dot-notation path
    type: string
    required: true
    
  - name: event_time
    path: context.created_at
    type: timestamp                   # RFC3339 → time.Time
    required: true
    
  - name: input_tokens
    path: properties.input_tokens
    type: int64
    default: 0
```

### Supported Types

| Type | JSON Source | Go Type |
|------|-------------|---------|
| `string` | string | `string` |
| `int64` | number | `int64` |
| `float64` | number | `float64` |
| `bool` | boolean | `bool` |
| `timestamp` | string (RFC3339) | `time.Time` |

---

## Ack/Nack Callbacks

Sinks receive ack/nack callbacks via `BuildOptions` during construction:

```go
type EmitFn func(ctx context.Context, tok *pb.CheckpointToken)
type NackFn func(ctx context.Context, frame *pb.Frame, err error)

type BuildOptions struct {
    Ack  EmitFn
    Nack NackFn
}

type Capabilities struct {
    AckAware  bool  // Sink will call Ack callback
    NackAware bool  // Sink will call Nack callback
}
```

When delivery is confirmed, sink calls `opts.Ack(tok)` → coordinator commits offset.

On permanent failure, sink calls `opts.Nack(frame, err)` → frame routes to DLQ.

---

## Registration

Sinks register via blank import:

```go
// cmd/engine/main.go
import (
    _ "quanta/sink/kafka"
    _ "quanta/sink/s3"
    _ "quanta/sink/clickhouse"
    _ "quanta/sink/stdout"
)
```

Each sink package has `register.go`:

```go
func init() {
    sink.Register(sink.Registration{
        Name:         "clickhouse",
        DecodeConfig: decodeConfig,
        New:          newClickHouseSink,
    })
}

func decodeConfig(raw any) (any, error) {
    var cfg Config
    if err := config.DecodeYAML(raw, &cfg); err != nil {
        return nil, err
    }
    return cfg, nil
}

func newClickHouseSink(ctx context.Context, raw any, opts sink.BuildOptions) (sink.Adapter, error) {
    cfg := raw.(Config)
    return newDriver(ctx, cfg, opts)
}
```

---

## Publish Semantics

- `Publish` must respect context cancellation
- Frames are immutable; batching must copy payloads
- AckAware sinks: return immediately, ack async
- Synchronous sinks: return = implicit ack

## Shutdown

- `Close` flushes pending batches and releases resources
- AckAware sinks drain in-flight acknowledgements
- NackAware sinks flush pending nacks to DLQ
