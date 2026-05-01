# S3 Sink Configuration

Amazon S3 (and compatible) batched file uploads with JSONL or Parquet format.

## Configuration Reference

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `bucket` | string | Yes | - | S3 bucket name |
| `region` | string | Yes* | - | AWS region (*or endpoint) |
| `prefix` | string | No | `""` | Key prefix (folder) |
| `file_suffix` | string | No | `.jsonl` | File extension |
| `format` | string | No | `jsonl` | Output format: `jsonl` or `parquet` |
| `schema_file` | string | Parquet only | - | Path to schema YAML |
| `batch_size` | int | No | `100` | Records per file |
| `flush_interval` | duration | No | `5s` | Max time before flush |
| `auth_strategy` | string | Yes | - | `static`, `iam-role`, or `env` |
| `access_key_id` | string | static only | - | AWS access key |
| `secret_access_key` | string | static only | - | AWS secret key |
| `endpoint` | string | No | - | Custom endpoint (LocalStack, MinIO) |
| `path_style` | bool | No | `false` | Use path-style URLs |
| `compression` | string | No | - | File compression (Parquet uses Snappy) |
| `encryption_sse` | string | No | - | Server-side encryption |
| `kms_key_id` | string | No | - | KMS key for SSE-KMS |

## Formats

### JSONL (Default)

Newline-delimited JSON. No schema required.

```yaml
sink_configs:
  s3:
    bucket: quanta-output
    region: us-east-1
    prefix: events/
    format: jsonl
    batch_size: 100
    flush_interval: 5s
    auth_strategy: iam-role
```

Output:

```
s3://quanta-output/events/2025-05-01T10-00-00Z_batch001.jsonl
```

### Parquet

Columnar format for analytics. Requires schema file.

```yaml
sink_configs:
  s3:
    bucket: quanta-output
    region: us-east-1
    prefix: events/

    format: parquet
    schema_file: topology/schemas/ai_events.schema.yaml

    batch_size: 1000
    flush_interval: 10s
    auth_strategy: static
    access_key_id: ${AWS_ACCESS_KEY_ID}
    secret_access_key: ${AWS_SECRET_ACCESS_KEY}
```

Output:

```
s3://quanta-output/events/2025-05-01T10-00-00Z_batch001.parquet
```

**Parquet Features:**

- Snappy compression (Spark/Databricks/BigQuery compatible)
- DataPageVersion 2 for better encoding
- Microsecond timestamp precision
- Schema-driven column mapping

## Authentication Strategies

### Static Credentials

```yaml
auth_strategy: static
access_key_id: AKIAIOSFODNN7EXAMPLE
secret_access_key: wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY
```

### IAM Role (EC2/ECS/Lambda)

```yaml
auth_strategy: iam-role
# Credentials from instance metadata
```

### Environment Variables

```yaml
auth_strategy: env
# Uses AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY
```

## LocalStack Example

```yaml
sink_configs:
  s3:
    bucket: quanta-output
    region: us-east-1
    prefix: events/
    format: jsonl
    batch_size: 100
    flush_interval: 5s
    auth_strategy: static
    access_key_id: test
    secret_access_key: test
    endpoint: http://localhost:4566
    path_style: true
```

## Schema File Example

Required for Parquet format. ODCS-aligned structure.

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

## Behavior Notes

- Files are named with ISO8601 timestamp + batch ID
- Batch flushes on size OR interval (whichever first)
- Upload failures trigger nack → DLQ routing
- Content-Type set automatically per format
