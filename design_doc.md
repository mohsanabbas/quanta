
---

# Quanta System Architecture and Design

## Overview

Quanta is a modular streaming/event‑processing engine written in Go.  It consumes events from a source (currently Kafka), runs them through an ordered chain of transformer plugins and forwards the transformed events to one or more sinks.  A gRPC control plane allows external clients to deploy/pause pipelines and check liveness, while a Prometheus metrics endpoint exposes runtime metrics for observability.  The architecture is intentionally pluggable: sources, transformers and sinks implement simple interfaces so new transports or back‑ends can be added with minimal changes to the core engine.

## High‑Level Architecture

At a high level the system consists of several distributed components connected via gRPC or message‑queue protocols.  The diagram below shows how data and control signals flow between them:

```
                    +----------------------+
                    |    Control Clients   |
                    +----------+-----------+
                               |           \
                               | gRPC + HTTP \
                               v             \
+----------------------+    +--------------------------+
|    Kafka Broker(s)   |--->|    Quanta Engine         |
+----------+-----------+    |  (Pipeline Runner)       |
           |                | +--------------------+   |
           | events         | |    Source Adapter  |   |
           v                | +---------+----------+   |
 +--------------------+     |           |              |
 | Transformer        |<---+-----------+|              |
 | Plugin Processes   |     |           v              |
 +--------------------+     |  +-------+----------+    |
                            |  | Transform Stages |    |
                            |  +--------+---------+    | 
                            |           |              |
                            |           v              |
                            |     +-----------+        |
                            |     |   Sinks   |       |
                            |     +-----------+        |
                            +--------------------------+ 
```

**Figure 1 – High‑level architecture.**  The Quanta engine sits between the Kafka broker and the sinks.  It pulls events from Kafka, invokes transformer plugins via gRPC, and pushes the results to sinks.  Control clients communicate with the engine’s gRPC server to deploy/pause pipelines and scrape metrics.  Each transformer stage may connect to a different plugin process.

### Explanation of Components

* **Kafka broker(s)** – External message queues from which the engine reads records.  The source adapter encapsulates the Kafka client and translates each record into a `Frame` object with a payload, timestamp and checkpoint information.

* **Quanta engine** – A single Go process comprising the bootstrap logic, pipeline runner, control gRPC server and metrics HTTP endpoint.  The engine loads a pipeline specification from YAML, constructs a `Runner` with a source, an ordered list of transformer stages and one or more sinks, then starts processing frames.

* **Transformer plugin processes** – External binaries that implement the `TransformService` defined in `transformer.proto`.  Each receives a `TransformRequest` and returns a `TransformResponse` with zero or more events and a status (OK/DROP/RETRY/ERROR).  The runner connects to each plugin via gRPC using a separate `transform.Client` instance.

* **Sinks** – Components that emit frames to downstream systems.  The current implementation provides a `stdout` sink  future sinks may write to Kafka, HTTP endpoints or storage services.  Each sink implements an adapter interface with methods `Configure`, `Push` and `Close`.

* **Control & Metrics** – The engine exposes a gRPC server that implements ping/deploy/pause and health checks, and an HTTP endpoint that exposes Prometheus metrics.  Control clients use these APIs to manage pipelines and monitor health.

## Engine Bootstrap

The engine bootstrap code (in `internal/engine`) performs the following steps:

1. **Load configuration** – Reads environment variables and command‑line flags to determine the gRPC and metrics ports and the path to the pipeline YAML.  It sets up TLS if required.
2. **Start transport server** – Launches a gRPC server that serves control, connector and health RPCs.  The control service implements ping, deploy and pause operations  the connector service allows sinks to acknowledge checkpoints back to the source  the health service reports liveness.
3. **Compile the pipeline** – Loads the YAML specification and constructs a `Runner` with a source adapter, transformer stages and sinks.  The compiler dials each plugin address and wraps it in a `transform.Client`.
4. **Start metrics endpoint** – Exposes Prometheus counters and histograms via an HTTP server.
5. **Run the pipeline** – Invokes `Runner.Start(ctx)` to begin consuming frames.  When the context is cancelled, it gracefully stops the runner and the gRPC server.

The bootstrap orchestrates these actions so that the engine is ready to process events before it accepts control requests.

## Pipeline Specification and Compilation

Pipelines are described in YAML and parsed into a `spec.File` struct.  The schema includes:

* `schema_version` – currently `v1`.
* `source` – defines the source type (`kafka`), driver (`sarama` or `kgo`) and configuration file.
* `transformers` – an ordered list of transformer specifications, each with a name, type (`grpc` or `inproc`), address, timeout and retry settings.
* `sinks` – one or more sink identifiers (e.g. `stdout`).
* `sink_configs` and `debug` – optional configuration sections for sinks and debugging.

During compilation (`internal/pipeline/compiler.go`), the code:

1. Validates the schema version and loads the Kafka config.  It instantiates the Kafka adapter and registers an ACK handler.
2. Iterates over the list of transformers.  For each, it dials the plugin (if type is `grpc`), constructs a `transform.Client` and adds it as a stage in the runner with the specified timeout and retry/backoff.
3. Creates sink adapters based on the sink names and their configuration.  Only the `stdout` sink is currently supported.
4. Returns the fully configured `Runner` ready to start.

This approach allows multiple transformers to be configured for a single pipeline.  The order in the YAML determines the order of execution: events flow through each stage sequentially.

## Runner and Frame Processing

The `Runner` is responsible for pulling frames from the source, applying transformations and pushing the results to sinks.  It maintains slices of sources, transformer stages and sinks, along with ACK handling.  The following ASCII diagram illustrates the runner’s internal structure and data flow:

```
             +-----------------------------------------------------------+
             |                          Runner                          |
             +------------------+------------------+--------------------+
             |     Source       |   Transform      |       Sinks        |
             |     Adapter      |     Stages       |                    |
             +---------+--------+---------+--------+---------+----------+
                       |                  |                    |
          Frame ──────►|                  |                    |
                       v                  v                    v
        +------------+    +----------------------------+   +-----------+
        |  Source    |    |  TransformStage[0]         |   |  Sink[0]  |
        |  Adapter   |    |  - name: upper             |   |  (stdout) |
        +------+-----+    |  - client: gRPC client     |   +-----------+
               |          |  - timeout: 1s             |
               v          |  - retries: 3              |
        Kafka record      |  - backoff_ms: 200         |
        converted to      +--------------+-------------+
        Frame             |              v
                          |       gRPC call to plugin
                          v              
                +----------------------------+       +-----------+
                | TransformStage[1]          |  ...  |  Sink[n]  |
                |  - name: enrich            |       +-----------+
                |  - client: gRPC client     |
                |  - timeout: 2s             |
                |  - retries: 5              |
                +----------------------------+
```

**Figure 2 – Runner internal structure.**  Each incoming frame is passed sequentially through the source adapter and each transform stage.  A `TransformStage` holds its name, a `transform.Client`, timeout and retry policy.  The runner’s `pushFrame` method converts a frame into a `TransformRequest`, calls the plugin via gRPC, handles statuses (OK, DROP, RETRY, ERROR) and converts returned events back into frames.  Only after a frame has successfully traversed all stages is it forwarded to sinks and acknowledged to the source.

## Kafka Source Adapter

The Kafka adapter (in `source/kafka`) abstracts the details of consuming records from a Kafka cluster.  It exposes a `Run(ctx, emit func(*Frame))` method that subscribes to the configured topic(s), converts each record into a `Frame` and calls the supplied `emit` callback.  The `Frame` includes:

* **Value** – event payload (as bytes).
* **Key** – partitioning key.
* **Headers** – map of Kafka headers.
* **Ts** – timestamp.
* **Checkpoint** – topic, partition and offset, used for acknowledging processed frames.

The adapter runs in a dedicated goroutine.  When the runner acknowledges a frame, the adapter commits the corresponding offset to Kafka to ensure at‑least‑once semantics.

## Transformer Client Abstraction

Transformer stages are decoupled from their transport via the `transform.Client` interface, which defines:

* `Metadata(ctx)` – returns plugin metadata such as name, version and capabilities.
* `Health(ctx)` – returns plugin health status.
* `Transform(ctx, *TransformRequest)` – unary RPC that transforms a single request into a response.
* `Stream(ctx, ...)` – bidirectional streaming RPC (future use).
* `Close()` – closes underlying connections.

The default implementation is a gRPC client (`GRPCClient`) that dials the plugin’s address and forwards calls to the generated gRPC stub.  An `InProcessClient` wraps a Go implementation compiled into the engine.  Additional transports such as stdio or shared memory can be added by implementing this interface.

## Transformer Plugins

Transformer plugins are external processes that implement the `TransformService` defined in `transformer.proto`.  Each plugin can be written in any language that supports gRPC and Protocol Buffers.  The key RPCs are:

* `Transform(TransformRequest) returns (TransformResponse)` – synchronous transform for individual events or batched requests.  The request includes the pipeline ID, plugin ID, payload and event metadata  the response returns zero or more events and a status (OK/DROP/RETRY/ERROR).
* `TransformStream(stream TransformStreamMessage)` – bidirectional streaming for high throughput (not yet used by the engine).
* `Health` and `Metadata` – liveness and capability queries.

A typical plugin parses the payload, applies domain logic and returns transformed events.  It may enrich events, filter them or call external services.  Plugins should respect deadlines and cancellation propagated via gRPC contexts to avoid blocking the runner.

## Sinks

Sinks consume frames emitted by the runner and forward them to downstream systems.  Each sink implements an adapter interface with methods:

* `Configure(ctx, spec)` – initialises the sink with configuration from the YAML file.
* `Push(*Frame) error` – sends a frame to the sink.  The sink may batch frames or perform asynchronous writes.
* `Close()` – flushes outstanding data and releases resources.
* `BindAck(func(*ConnectorAck))` – optional: binds a callback that allows the sink to acknowledge processed frames back to the source via the connector service.

The current implementation includes an `stdout` sink that prints each frame and batches acknowledgements.  Additional sinks can be implemented to write to Kafka producers, HTTP endpoints, filesystems or databases.

## Control Plane & Metrics

The engine exposes a gRPC control plane and a health check service defined in `control.proto` and `health.proto`.  Control clients can:

* **Ping** – check if the engine is responding.
* **Deploy** – push a new pipeline specification to the engine (not yet fully implemented).
* **Pause/Resume** – pause or resume a running pipeline.
* **Liveness check** – query the engine’s health status.

Prometheus metrics are exposed via an HTTP endpoint.  Counters track total processed frames, dropped frames, retry counts and errors per stage  histograms measure processing latency.  These metrics allow operators to monitor pipeline health and tune performance.

## Distributed Components & Communication

The following ASCII diagram shows how distributed components communicate over the network:

```
    Kafka Protocol (TCP)          gRPC per transformer            Sink Protocol
   +------------------+           +---------------------+          +-----------------+
   |  Kafka Broker(s) |--------->|  Quanta Engine      |---------->|  Sink Adapter   |
   +------------------+          +---------+-----------+           +---------+-------+
                                      |   |                            |
                                      |   | gRPC                       |
                                      |   v                            |
                                      | +-------------------+          |
                                      +-| Transformer Plugin|          |
                                        +-------------------+          |
                                              |                        |
                                              gRPC                     |
                                              |                        |
                                              v                        v
                                       +------------------+    +-----------------+
                                       | Control Clients  |    | Prometheus      |
                                       +------------------+    +-----------------+
```

**Figure 3 – Distributed communications.**  The source adapter consumes records from Kafka over the Kafka protocol.  For each transform stage, the runner establishes a gRPC connection to the plugin process.  After transformation, frames are delivered to sinks using the sink’s protocol (stdout is local  future sinks may use HTTP or another Kafka producer).  Separate control clients manage pipelines and scrape metrics via gRPC/HTTP.

## Frame Lifecycle

The lifecycle of a frame illustrates how events traverse the system:

```
Kafka record
     |
     v
Kafka Adapter → Frame → Runner
     |
     v
TransformRequest → gRPC → Plugin
     |
     v
TransformResponse → Events → Frame(s)
     |
     v
Sink(s) → Output + optional ACK
```

1. A record is consumed from Kafka and converted into a `Frame` by the source adapter.
2. The runner wraps the payload and metadata into a `TransformRequest` and calls the first transformer stage.  The request includes the pipeline ID, plugin ID, payload and event metadata.
3. The plugin processes the request and returns a `TransformResponse` with zero or more events and a status.  The runner handles `OK`, `DROP`, `RETRY` and `ERROR` statuses accordingly.
4. The runner converts each returned event back into a `Frame` and passes it to the next stage.  This process repeats for all stages.
5. When all stages succeed, the resulting frames are pushed to sinks.  Sinks may emit acknowledgements to the source via the connector service, allowing the Kafka adapter to commit offsets.

## Extensibility and Scalability

Quanta’s design intentionally separates concerns via interfaces.  New transport modes (e.g. shared memory or IPC), new sink types, and new source adapters can be added without modifying the core runner.  The ordered list of transformers allows pipelines to express complex transformations by composing small, reusable plugins.  Each stage can specify its own timeout and retry/backoff policy, enabling careful tuning of performance and reliability.

Multiple pipelines can be hosted by a single engine, each with its own set of transformers.  Currently, each pipeline opens its own gRPC connection to each plugin.  In the future, connections could be pooled so that multiple pipelines share a connection and multiplex their requests, reducing resource overhead.

## Summary

This document has described the architecture of the Quanta event‑processing engine in detail.  Quanta reads events from Kafka, processes them through an ordered chain of transformer plugins and sends the results to sinks.  A modular design with well‑defined interfaces for sources, transformers and sinks enables easy extension and customization.  The control plane and metrics endpoint provide operational visibility and management.  By decoupling stages via gRPC and Protobuf contracts, Quanta supports polyglot plugin development and can evolve towards high‑performance transports such as streaming and shared memory.
