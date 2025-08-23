Below is a **flattened, idiomatic Go layout** that keeps every concern-separation you asked for (driver ↔ back-pressure ↔ checkpoint) **without deep nesting**.
The design follows the widely-used *golang-standards/project-layout* convention (`cmd/`, `internal/`, `pkg/`, etc.), so editors, tooling, and new contributors will feel at home.

```
quanta/
├── cmd/                         # Binaries
│   ├── engine/                  # quanta-engine main()
│   └── ctl/                     # quanta-ctl  (Admin CLI)
│
├── api/                         # Protobufs & generated stubs
│   └── proto/v1/
│       ├── frame.proto
│       ├── connector.proto
│       ├── control.proto
│       └── health.proto
│
├── internal/                    # Private application code (not for import)
│   ├── engine/                  # Orchestrator, DAG execution, shutdown
│   ├── pipeline/                # Compiler / runtime state-machine
│   ├── transport/               # gRPC & (opt) shared-memory implementations
│   ├── telemetry/               # Metrics, tracing, structured logging
│
│   └── source/                  # ------ Kafka SOURCE lives here -------------
│       ├── kafka/               # single, flat, well-scoped package
│       │   ├── adapter.go       # SourceAdapter interface
│       │   ├── registry.go      # picks driver by `lib:` cfg key
│       │   ├── backpressure.go  # token-bucket / credit algorithms
│       │   ├── checkpoint.go    # commit, rewind, pluggable stores
│       │   ├── config.go        # unified YAML→struct decode
│       │
│       │   ├── driver_kgo.go        // +build kgo
│       │   ├── driver_sarama.go     // +build sarama
│       │   └── driver_confluent.go  // +build confluent
│       │
│       └── ... (future sources share same pattern)
│
│   └── sink/                    # Built-in sinks (mongodb, redis, …)
│
├── pkg/                         # Public SDKs (usable by other Go modules)
│   ├── connector/               # sdk.NewSource(), sdk.ServeSource() …
│   └── checkpoint/              # Exports checkpoint client for custom sinks
│
├── configs/                     # Example YAML/HCL configs
│   └── kafka_source.yaml
│
├── examples/                    # End-to-end demos (docker-compose, k8s, …)
│   └── kafka-error-only-filter/
│
├── deployments/                 # Dockerfiles / Helm charts / K8s manifests
├── scripts/                     # CI, lint, proto-gen helpers
├── test/                        # Integration & load tests
├── Makefile                     # make build / make test / make proto
├── go.mod
└── go.sum
```

---

## 🗝️  Why this layout is “idiomatic Go”

| Principle (per `golang-standards/project-layout`) | How it’s met here                                                                                           |
| ------------------------------------------------- | ----------------------------------------------------------------------------------------------------------- |
| **`cmd/` = `.main()` packages only**              | `cmd/engine` and `cmd/ctl` are the *only* binaries.                                                         |
| **`internal/` hides app logic**                   | All Kafka logic, pipeline, engine, etc. are inside `internal/`, preventing accidental external imports.     |
| **Flat packages, few sub-folders**                | `internal/source/kafka` holds **everything** Kafka-specific; each driver is just one file with a build-tag. |
| **`pkg/` exports stable APIs**                    | Only the lightweight SDK (connector, checkpoint) is made importable.                                        |
| **Config, docs, scripts at the top**              | `configs/`, `examples/`, `scripts/`, `deployments/` live at project root.                                   |

---

## ⚙️  Inside **`internal/source/kafka`**

```
adapter.go
  └── type Adapter interface {
        Configure(Config) error
        Run(ctx context.Context, emit EmitFunc) error
      }

registry.go
  └── func NewAdapter(lib string) (Adapter, error)

backpressure.go
  └── func NewController(cfg BPConfig) *Controller
      // credit-based flow control shared by all drivers

checkpoint.go
  └── func NewManager(cfg CPConfig) *Manager
      // commit & rewind, with pluggable stores

driver_kgo.go        // build tag: kgo
driver_sarama.go     // build tag: sarama
driver_confluent.go  // build tag: confluent
```

* **One package path** (`quanta/internal/source/kafka`).
  IDE auto-completion is painless; no “import π/σ/τ/κ/go/kafka/driver/kgo” spaghetti.
* **Build tags** let you ship *all* drivers in one repo while each binary picks only what it needs:

```bash
go build -tags=kgo   ./cmd/engine      # engine w/ kgo
go build -tags=sarama ./cmd/connector  # connector harness w/ Sarama
```

---

## 🔄  Typical Data Flow

```
driver_kgo.Run()
        │    (batch []*kgo.Record)
        ▼
backpressure.Controller.Allow(n)   ← global credits
        │    (Frames)
        ▼
checkpoint.Manager.CommitOffsets()
        │
        ▼
sdk.Emit(frame) → internal/engine
```

Exactly-once or at-least-once is controlled by the **checkpoint store** (Kafka, Redis, file, etc.), *shared* with sinks via `pkg/checkpoint`.

---

## 🧪  Where to put tests

* **Unit tests** live right beside code (`kafka/adapter_test.go`).
* **Integration / soak** live in `test/`, spin up Kafka via Docker Compose (testcontainers-go).

---

## 🚀  Next-step scaffolding

1. **Interfaces & stubs**: generate `adapter.go`, `backpressure.go`, `checkpoint.go`.
2. **Config loader**: wrap Viper or Koanf in `internal/source/kafka/config.go`.
3. **CI lint/test**: add `scripts/ci.sh` + GitHub Actions.

---

Tell me which piece you’d like first (e.g., the `Adapter` interface code, build-tag examples, or a minimal `driver_kgo.go`) and we’ll dive straight in!
