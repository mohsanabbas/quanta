
---

# Quanta System Design Document

## Overview

Quanta is a modular pipeline engine designed to process event streams with high flexibility and observability. It reads events from a source (currently Kafka), applies a sequence of transformer plugins, and forwards the transformed events to one or more sinks. The system includes a gRPC control/health server and an HTTP metrics endpoint for orchestration and monitoring.

---

## High-Level Architecture

### Core Components

| Component            | Description |
|----------------------|-------------|
| **Engine Process**   | Go binary launched via `cmd/engine/main.go`. Reads pipeline YAML, bootstraps the runner, starts gRPC control server, and exposes Prometheus metrics. |
| **Sources**          | Kafka adapter that consumes records and wraps them into `Frame` messages. |
| **Transformers**     | Optional gRPC plugins that process events. Defined in `transformer.proto`, they return transformed events and status codes (OK, DROP, RETRY, ERROR). |
| **Sinks**            | Consumers of transformed frames. Currently only a stdout sink is implemented. |
| **Pipeline Runner**  | Orchestrates the flow: pulls frames, applies transformers with retries/backoffs/timeouts, and pushes to sinks. |
| **Control/Health Servers** | gRPC services for ping, deploy, pause, and liveness checks. |
| **Metrics Endpoint** | HTTP server exposing Prometheus metrics at a configurable port. |

---

## Component Deep Dive

### 1. Engine Bootstrap (`internal/engine`)
- Reads `engine.Config` (gRPC port, metrics port, pipeline YAML path).
- Starts transport and metrics servers.
- Calls `pipeline.Compile()` to build a `Runner`.
- Invokes `Runner.Start()` to begin processing.
- Gracefully shuts down transport and clients.

### 2. Configuration Loading
- YAML spec parsed by `internal/config/pipeline.go` into `spec.File`.
- Includes `schema_version`, source, transformers, sinks, and debug options.
- Compiler dials each transformer plugin and registers it with timeout/retry policies.
- Unsupported transformer types trigger compile errors.

### 3. Pipeline Runner (`internal/pipeline/runner.go`)
- Holds source, sinks, and ordered `transformStage` objects.
- Each stage includes transformer name, gRPC client, timeout, and retry/backoff settings.
- For each incoming `Frame`:
  - Converts to `TransformRequest` with metadata.
  - Invokes `client.Transform()` per stage with retries.
  - Handles status codes:
    - OK → forward events
    - DROP → acknowledge and discard
    - RETRY/ERROR → retry or drop after exhaustion
  - Converts `Events` back to `Frames` and forwards.
- On shutdown, closes all transformer clients and sinks.

### 4. Kafka Source Adapter (`source/kafka`)
- Implements `kafka.Adapter` with `Run(ctx, emit func(*Frame) error)`.
- Consumes Kafka records, wraps them into `Frame` objects, and emits to runner.

### 5. Transformer Client Abstraction (`internal/transform/plugin.go`)
- Defines `transform.Client` interface: `Metadata`, `Health`, `Transform`, `Stream`, `Close`.
- Provides:
  - `NewGRPCClient(ctx, address)` for remote plugins.
  - In-process client for compiled plugins.
  - Extensible transport layer (e.g., stdio, shared memory).

### 6. Transformer Plugins
- Standalone processes implementing `TransformService` from `transformer.proto`.
- Communicate via TCP or Unix sockets.
- Accept `TransformRequest`, return `TransformResponse`.
- Can emit multiple events, drop, retry, or error.
- Language-agnostic (any gRPC/protobuf-compatible language).

### 7. Sinks
- Implement `sink.Adapter` interface: `Configure`, `Push(*Frame)`, `Close`, optional `BindAck`.
- Stdout sink supports batching and flush intervals.
- Future sinks (Kafka, S3, HTTP) can follow the same pattern.

### 8. Control Plane & Metrics
- Control service: RPCs for ping, deploy, pause (stubbed in `control.proto`).
- Health service: simple liveness check.
- Metrics endpoint: exposes counters and histograms for Prometheus scraping.

---

## Distributed Components & Communication

### Processes

| Process              | Role |
|----------------------|------|
| **Engine**           | Runs Kafka consumer, pipeline runner, sinks, control/metrics servers. Holds one gRPC client per transformer stage. |
| **Kafka Broker(s)**  | External cluster for source ingestion and future sink output. Communicates via Kafka protocol over TCP. |
| **Transformer Plugins** | Separate binaries exposing gRPC services. Each pipeline opens its own gRPC connection (optimization possible). |

### Optional Components
- External sinks (e.g., HTTP endpoints, S3).
- In-process plugins (compiled into engine, avoiding network overhead).

---

## Data Flow & Connections

```
Kafka Broker  --(TCP/Kafka)-->  Engine (Source)
Engine (Runner)  --(gRPC per transformer stage)-->  Plugin Process(es)
Engine (Runner)  --(Sink Protocol)-->  Sinks (stdout, HTTP, etc.)
Engine (Transport Server)  <--(gRPC)-->  Control Clients (CLI)
Engine  --(HTTP)-->  Metrics Scraper (Prometheus)
```

### Frame Lifecycle

1. Kafka adapter consumes a record → emits to runner.
2. Runner converts to `TransformRequest` → invokes each transformer.
3. Plugin returns `Events` + status → runner converts back to `Frames`.
4. Runner forwards to next stage or sinks.
5. Sinks may acknowledge via `ConnectorAck`.

---

## Extensibility

- Each transformer stage is abstracted via `transform.Client`.
- New transports (e.g., IPC) or in-process plugins can be added by implementing the interface.
- New sinks/sources only require conforming to their respective adapter interfaces.

---

## Summary

Quanta delivers a clean, extensible pipeline architecture with:
- Kafka source ingestion
- gRPC-based transformer chain
- Sink layer with structured output
- Configurable retries, timeouts, and backoff
- Modular interfaces for easy extension

While each pipeline currently opens its own gRPC connection per transformer, future optimization could enable connection sharing. The transformer interface is highly flexible, allowing new transports or embedded implementations with minimal changes to the engine.

---
