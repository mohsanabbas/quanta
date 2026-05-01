# Quanta System Design Document

## Overview

Quanta is a modular streaming engine that processes events from Kafka through gRPC transformers and delivers to multiple sinks with end-to-end delivery guarantees.

---

## Architecture

```
┌─────────────────────────────────────────────────────────────────────────┐
│                              Engine                                     │
│                                                                         │
│  ┌─────────────┐    ┌─────────────┐    ┌─────────────────────────────┐  │
│  │   Kafka     │───▶│   Runner    │───▶│          Sinks              │  │
│  │   Source    │    │             │    │  ┌───────┐ ┌────┐ ┌──────┐  │  │
│  └─────────────┘    │             │    │  │ Kafka │ │ S3 │ │ CH   │  │  │
│         │           └──────┬──────┘    │  └───┬───┘ └──┬─┘ └───┬──┘  │  │
│         │                  │           └──────┼────────┼───────┼─────┘  │
│         │             ┌────▼────┐             │        │       │        │
│         │             │  gRPC   │             │        │       │        │
│         │             │Transform│             │        │       │        │
│         │             └─────────┘             │        │       │        │
│         │                                     │        │       │        │
│         │    ┌────────────────────────────────┼────────┼───────┼─────┐  │
│         └───▶│         AckCoordinator         │        │       │     │  │
│              │  Barrier(tok, refs) ───────────┴────────┴───────┘     │  │
│              │           │                                           │  │
│              │           ▼                                           │  │
│              │     commit(offset) ──────────────────▶ Source         │  │
│              └───────────────────────────────────────────────────────┘  │
│                                                                         │
│  ┌─────────────┐    ┌─────────────┐                                     │
│  │   gRPC      │    │   HTTP      │                                     │
│  │  Control    │    │  Metrics    │                                     │
│  └─────────────┘    └─────────────┘                                     │
└─────────────────────────────────────────────────────────────────────────┘
```

---

## Core Components

| Component | Description |
|-----------|-------------|
| **Engine Process** | Go binary: parses config, bootstraps runner, starts gRPC control and HTTP metrics |
| **Kafka Source** | Sarama consumer wrapping records into Frame messages |
| **Transformers** | gRPC plugins returning OK/DROP/RETRY/ERROR status codes |
| **Sinks** | Delivery adapters with ack/nack semantics |
| **AckCoordinator** | Refcounted barriers controlling offset commits |
| **Schema Mapper** | ODCS-aligned JSON-to-column extraction |

---

## Sink Adapters

| Sink | Protocol | Format | Batching | Use Case |
|------|----------|--------|----------|----------|
| **Kafka** | TCP | Binary | Producer batching | Event streaming, fan-out |
| **S3** | HTTPS | JSONL, Parquet | File batching | Data lake, analytics |
| **ClickHouse** | Native TCP | Columnar | Batch inserts | Real-time OLAP |
| **Stdout** | - | Text | - | Debugging |

### Sink Interfaces

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

### AckAware Flow

```
Frame ──▶ Publish() ──▶ [async delivery] ──▶ Ack(tok) ──▶ Coordinator
                                                              │
                                                       refs = 0
                                                              │
                                                              ▼
                                                        commit(offset)
```

### NackAware Flow

```
Frame ──▶ Publish() ──▶ [delivery fails] ──▶ Nack(frame, err) ──▶ DLQ
```

---

## Schema-Driven Sinks

S3 (Parquet) and ClickHouse use `sink/schema` for JSON-to-column mapping:

```
JSON Event ──▶ Schema Mapper ──▶ Typed Values ──▶ Sink
                    │
                    ▼
            ODCS Schema YAML
              (columns, paths, types)
```

### Schema File Structure

```yaml
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
```

### Supported Types

| Type | JSON → Go |
|------|-----------|
| `string` | string → string |
| `int64` | number → int64 |
| `float64` | number → float64 |
| `bool` | boolean → bool |
| `timestamp` | RFC3339 string → time.Time |

---

## S3 Sink Details

### Formats

| Format | Content-Type | Schema Required |
|--------|--------------|-----------------|
| `jsonl` | `application/x-ndjson` | No |
| `parquet` | `application/vnd.apache.parquet` | Yes |

### Parquet Features

- Snappy compression (Spark/Databricks compatible)
- DataPageVersion 2 for better encoding
- Microsecond timestamp precision
- Schema-driven column mapping

### Batch Flow

```
Frame ──▶ Flusher.Add() ──▶ batch fills ──▶ Seal
                                              │
                                              ▼
                                    Encoder.Encode(records)
                                              │
                                              ▼
                                    s3.PutObject(key, data)
                                              │
                                       ┌──────┴──────┐
                                       ▼             ▼
                                   Success        Failure
                                       │             │
                                       ▼             ▼
                                    Ack(tok)     Nack(frame)
```

---

## ClickHouse Sink Details

### Authentication Strategies

| Strategy | Use Case | Credentials |
|----------|----------|-------------|
| `native` | Simple deployments | username + password/password_env |
| `tls` | Production mTLS | client_cert + client_key + ca_cert |
| `env` | K8s secrets | CH_USER, CH_PASSWORD env vars |

### Security

- **Never logged**: hosts, username, password, certificates
- Env var precedence: `password_env` overrides `password`
- TLS validation enforced unless explicitly disabled

### Batch Insert Flow

```
Frame ──▶ Flusher.Add() ──▶ batch fills ──▶ Seal
                                              │
                                              ▼
                                    conn.PrepareBatch(INSERT...)
                                    batch.Append(row...)
                                    batch.Send()
                                              │
                                       ┌──────┴──────┐
                                       ▼             ▼
                                   Success        Failure
                                       │             │
                                       ▼             ▼
                                    Ack(tok)     Nack(frame)
```

---

## AckCoordinator

### Barrier State Machine

```
     Barrier(tok, refs)
            │
            ▼
      ┌──────────┐
      │   Live   │
      └────┬─────┘
           │
  ┌────────┴────────┐
  │                 │
refs ≤ 0         Abort()
  │                 │
  ▼                 ▼
┌──────────┐  ┌──────────┐
│Committed │  │ Aborted  │
└────┬─────┘  └────┬─────┘
     │             │
     ▼             ▼
 commit()      no commit
```

### Coordinator Sequence

```
Source ──emit──▶ Runner ──Barrier(tok, refs)──▶ Coordinator
                   │
                   ├──▶ Kafka Sink ──Ack──▶┐
                   ├──▶ S3 Sink ────Ack──▶ │ Coordinator
                   └──▶ ClickHouse ─Ack──▶┘
                                              │
                                         refs = 0
                                              │
                                              ▼
                                       commit(offset)
                                              │
                                              ▼
                                           Source
```

---

## Error Handling

### Three-Path Error Routing

| Path | Trigger | Destination |
|------|---------|-------------|
| **Plugin errors** | Transformer returns ERROR | Per-transformer error sink |
| **NackAware** | Sink delivery failure | Engine DLQ |
| **DeadLetterFn** | Infrastructure failure | Engine DLQ |

### NackAware Sink Flow

```
Sink ──delivery fails──▶ Nack(ctx, frame, err)
                              │
                              ▼
                        Coordinator
                              │
                       ┌──────┴──────┐
                       │             │
                  DLQ enabled    DLQ disabled
                       │             │
                       ▼             ▼
                   DLQ.Publish   withhold ack
                       │         (redelivery)
                       ▼
                  commit offset
```

---

## Configuration

### Pipeline Structure

```yaml
schema_version: v1

source:
  kind: kafka
  driver: sarama
  config: kafka_source.yml

transformers:
  - name: uppercase
    type: grpc
    address: "transformer:50052"

sinks:
  - kafka
  - s3
  - clickhouse

sink_configs:
  kafka: { ... }
  s3: { ... }
  clickhouse: { ... }

dlq:
  enabled: true
  sink: kafka
  config: { ... }
```

### Environment Precedence

1. **Defaults** (lowest)
2. **YAML files**
3. **Environment variables** (highest)

---

## Package Structure

```
sink/
├── batch/           # Generic Flusher[T] for batching
├── schema/          # ODCS-aligned JSON-to-column mapping
├── kafka/           # Kafka AsyncProducer
├── s3/              # S3 with JSONL/Parquet encoders
├── clickhouse/      # ClickHouse native protocol
└── stdout/          # Debug output
```

---

## Extensibility

- **New sinks**: Implement `sink.Adapter`, register via blank import
- **New formats**: Implement `Encoder` interface (S3)
- **New transports**: Implement `transform.Client` interface

---

## Summary

Quanta delivers:

- Multi-sink fan-out (Kafka, S3, ClickHouse)
- Schema-driven output (Parquet, ClickHouse)
- At-least-once delivery via AckCoordinator
- Secure credential handling (env vars, TLS)
- DLQ routing for delivery failures
