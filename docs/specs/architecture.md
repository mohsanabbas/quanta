# Architecture

## Overview

Quanta is a modular streaming engine that processes events from sources through transformers to sinks with end-to-end delivery guarantees.

```
┌─────────────┐     ┌─────────────┐     ┌─────────────────────┐
│   Source    │────▶│ Transformer │────▶│       Sinks         │
│   (Kafka)   │     │   (gRPC)    │     │ Kafka/S3/ClickHouse │
└─────────────┘     └─────────────┘     └─────────────────────┘
       │                                          │
       │           ┌───────────────┐              │
       └──────────▶│AckCoordinator │◀─────────────┘
                   │ (commit ctrl) │
                   └───────────────┘
```

## Core Components

| Component | Description |
|-----------|-------------|
| **Engine** | Go binary that bootstraps pipeline, gRPC control, and metrics |
| **Source** | Kafka consumer wrapping records into Frame messages |
| **Transformer** | gRPC plugins returning OK/DROP/RETRY/ERROR status |
| **Sink** | Delivery adapters: Kafka, S3 (JSONL/Parquet), ClickHouse |
| **AckCoordinator** | Refcounted barrier managing offset commits |

---

## Engine Lifecycle

1. **Build** – Parse config, resolve adapters, create AckCoordinator. No goroutines yet.
2. **Start** – `Runner.Start(ctx)` begins source consumption. Frames flow through transformers to sinks.
3. **Stop** – Context cancellation triggers graceful shutdown: sinks → transformers → source.

---

## Data Flow

```
┌──────────────────────────────────────────────────────────────────────┐
│                              Engine                                  │
│                                                                      │
│  ┌─────────┐    ┌─────────┐    ┌──────────────────────────────────┐  │
│  │  Kafka  │───▶│ Runner  │───▶│            Sinks                 │  │
│  │ Source  │    │         │    │ ┌────────┐ ┌─────┐ ┌───────────┐ │  │
│  └─────────┘    │         │    │ │ Kafka  │ │ S3  │ │ClickHouse │ │  │
│       │         └────┬────┘    │ └────┬───┘ └──┬──┘ └─────┬─────┘ │  │
│       │              │         └──────┼────────┼──────────┼───────┘  │
│       │              │                │        │          │          │
│       │         ┌────▼────┐           │        │          │          │
│       │         │  gRPC   │           │        │          │          │
│       │         │Transform│           │        │          │          │
│       │         └─────────┘           │        │          │          │
│       │                               │        │          │          │
│       │    ┌──────────────────────────┼────────┼──────────┼───────┐  │
│       │    │      AckCoordinator      │        │          │       │  │
│       │    │  ┌─────────────────────────────────────────────────┐ │  │
│       └────┼──│  Barrier(tok, refs) → Ack() → commit → Source   │─┘  │
│            │  └─────────────────────────────────────────────────┘    │
│            └─────────────────────────────────────────────────────────┘
└──────────────────────────────────────────────────────────────────────┘
```

---

## Sink Architecture

### Available Sinks

| Sink | Protocol | Format | Use Case |
|------|----------|--------|----------|
| **Kafka** | TCP | Binary | Event streaming, fan-out |
| **S3** | HTTPS | JSONL, Parquet | Data lake, analytics |
| **ClickHouse** | TCP (native) | Columnar | Real-time OLAP |
| **Stdout** | - | Text | Debugging |

### Schema-Driven Sinks

S3 (Parquet) and ClickHouse use `sink/schema` for JSON-to-column mapping:

```
JSON Event ──▶ Schema Mapper ──▶ Typed Columns ──▶ Sink
                    │
                    ▼
            ODCS Schema YAML
```

---

## AckCoordinator

The coordinator manages offset commits using refcounted barriers.

### Barrier State Machine

```
         Barrier(tok, refs)
                │
                ▼
         ┌──────────┐
         │   Live   │
         └────┬─────┘
              │
    ┌─────────┴─────────┐
    │                   │
refs ≤ 0            Abort()
    │                   │
    ▼                   ▼
┌──────────┐     ┌──────────┐
│Committed │     │ Aborted  │
└────┬─────┘     └────┬─────┘
     │                │
     ▼                ▼
  commit()       no commit
```

### Flow

```
Source ──emit──▶ Runner ──Barrier(tok, refs)──▶ Coordinator
                   │
                   ├──▶ Kafka Sink ──Ack(tok)──▶ Coordinator
                   ├──▶ S3 Sink ────Ack(tok)──▶ Coordinator
                   └──▶ ClickHouse ─Ack(tok)──▶ Coordinator
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

## Delivery Guarantees

| Mode | Behavior | Use Case |
|------|----------|----------|
| **Auto** | Commit on emit | High throughput, some loss OK |
| **E2E** | Commit after all sinks ack | At-least-once delivery |

---

## Context Propagation

- Source provides context per frame
- Transformers/sinks honor deadlines and cancellation
- Shutdown: cancel root context → operations unblock → graceful drain

---

## Backpressure

- Source: bounded semaphore (`Controller`) caps in-flight frames
- AckAware sinks: async publish, backpressure via outstanding barriers
- Synchronous sinks: inline completion, direct backpressure
