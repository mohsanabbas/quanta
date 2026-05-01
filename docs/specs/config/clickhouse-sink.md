# ClickHouse Sink Configuration

Real-time OLAP analytics via ClickHouse native protocol.

## Configuration Reference

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `host` | string | Yes* | - | Single host (`host:9000`) |
| `hosts` | []string | Yes* | - | Cluster hosts (*or host) |
| `database` | string | Yes | - | Target database |
| `table` | string | Yes | - | Target table |
| `schema_file` | string | Yes | - | Path to schema YAML |
| `auth_strategy` | string | No | `native` | `native`, `tls`, or `env` |
| `username` | string | native | - | ClickHouse username |
| `password` | string | No | - | Password (prefer `password_env`) |
| `username_env` | string | No | - | Env var for username |
| `password_env` | string | No | - | Env var for password |
| `tls` | bool | No | `false` | Enable TLS |
| `insecure_skip_verify` | bool | No | `false` | Skip TLS verification (dev only) |
| `ca_cert` | string | No | - | Path to CA certificate |
| `client_cert` | string | tls auth | - | Path to client certificate |
| `client_key` | string | tls auth | - | Path to client key |
| `batch_size` | int | No | `10000` | Records per batch insert |
| `flush_interval` | duration | No | `5s` | Max time before flush |
| `compression` | string | No | `lz4` | `none`, `lz4`, or `zstd` |
| `dial_timeout` | duration | No | `10s` | Connection timeout |
| `max_idle_conns` | int | No | `5` | Idle connection pool |
| `max_open_conns` | int | No | `10` | Max connections |
| `conn_max_lifetime` | duration | No | `1h` | Connection lifetime |

## Authentication Strategies

### Native (Username/Password)

```yaml
sink_configs:
  clickhouse:
    host: "clickhouse:9000"
    database: analytics
    table: ai_events
    schema_file: topology/schemas/ai_events.schema.yaml

    auth_strategy: native
    username: default
    password_env: CLICKHOUSE_PASSWORD  # Recommended
    # password: plaintext              # Not recommended
```

### TLS (mTLS with Client Certificates)

```yaml
sink_configs:
  clickhouse:
    host: "clickhouse:9440"
    database: analytics
    table: ai_events
    schema_file: topology/schemas/ai_events.schema.yaml

    auth_strategy: tls
    tls: true
    ca_cert: /etc/ssl/ca.pem
    client_cert: /etc/ssl/client.pem
    client_key: /etc/ssl/client.key
```

### Environment Variables

```yaml
sink_configs:
  clickhouse:
    host: "clickhouse:9000"
    database: analytics
    table: ai_events
    schema_file: topology/schemas/ai_events.schema.yaml

    auth_strategy: env
    username_env: CH_USER
    password_env: CH_PASSWORD
```

## Docker Development Example

```yaml
sink_configs:
  clickhouse:
    host: "clickhouse:9000"
    database: analytics
    table: ai_events
    schema_file: topology/schemas/ai_events.schema.yaml

    auth_strategy: native
    username: default
    password_env: CLICKHOUSE_PASSWORD

    tls: false  # Docker dev

    batch_size: 5000
    flush_interval: 5s
    compression: lz4
```

## Cluster Mode Example

```yaml
sink_configs:
  clickhouse:
    hosts:
      - "ch-node1:9000"
      - "ch-node2:9000"
      - "ch-node3:9000"
    database: analytics
    table: ai_events_distributed
    schema_file: topology/schemas/ai_events.schema.yaml

    auth_strategy: tls
    tls: true
    ca_cert: /etc/ssl/ca.pem

    batch_size: 10000
    flush_interval: 10s
    compression: zstd

    max_open_conns: 30
```

## Schema File Example

```yaml
# topology/schemas/ai_events.schema.yaml
kind: Schema
apiVersion: v1
name: ai_events
domain: ai-platform
owner: platform-team

columns:
  - name: event_id
    path: context.event_contract_id
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

  - name: input_tokens
    path: properties.input_tokens
    type: int64
    default: 0
```

## ClickHouse Table DDL

```sql
CREATE DATABASE IF NOT EXISTS analytics;

CREATE TABLE IF NOT EXISTS analytics.ai_events (
    event_id String,
    event_type LowCardinality(String),
    event_time DateTime64(6, 'UTC'),
    event_source String,
    provider LowCardinality(String),
    model String,
    request_id String,
    input_tokens Int64,
    output_tokens Int64,
    total_tokens Int64,
    latency_ms Int64,
    status LowCardinality(String),
    status_class LowCardinality(String),
    temperature Float64,
    stream_enabled UInt8,
    environment LowCardinality(String),
    org_id String,
    user_id String
) ENGINE = MergeTree()
ORDER BY (event_time, event_id)
PARTITION BY toYYYYMM(event_time);
```

## Security Notes

- **Never logged**: hosts, username, password, certificate paths
- Env var precedence: `password_env` overrides `password`
- Use `insecure_skip_verify: true` only in development
- Prefer mTLS (`auth_strategy: tls`) for production

## Compression

| Method | Speed | Ratio | Use Case |
|--------|-------|-------|----------|
| `lz4` | Fastest | Good | Default, high throughput |
| `zstd` | Fast | Best | Bandwidth-constrained |
| `none` | - | - | Debugging |

## Behavior Notes

- Uses native TCP protocol (port 9000), not HTTP
- Batch inserts: `PrepareBatch` → `Append` → `Send`
- Connection pooling with automatic reconnect
- Schema-driven column extraction via `sink/schema`
