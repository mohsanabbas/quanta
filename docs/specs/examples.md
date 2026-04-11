# Examples & Quickstarts

## Kafka → Transformer → Stdout

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
sinks:
  - stdout
sink_configs:
  stdout:
    print_counter: true
    print_value: true
```

Run the transformer and engine:

```sh
UPPERCASE_LISTEN_ADDR=:50052 go run ./examples/transformers/uppercase &
QUANTA_PIPELINE_YML=topology/pipeline.yml go run ./cmd/engine
```

## Kafka → Transformer → Kafka

```yaml
schema_version: v1
source:
  kind: kafka
  driver: sarama
  config: kafka_source.yml
transformers:
  - name: card-normaliser
    type: grpc
    address: "localhost:50052"
sinks:
  - kafka
sink_configs:
  kafka:
    brokers: ["kafka:29092"]
    topic: "normalized.events"
```

Produce and consume for smoke testing:

```sh
docker compose up -d
UPPERCASE_LISTEN_ADDR=:50052 go run ./examples/transformers/uppercase &
QUANTA_PIPELINE_YML=topology/pipeline.yml go run ./cmd/engine
```

## Kafka → Transformer → Kafka + DLQ

```yaml
schema_version: v1
source:
  kind: kafka
  driver: sarama
  config: kafka_source.yml
transformers:
  - name: cloudevents
    type: grpc
    address: "localhost:50052"
    error_sink:
      sink: kafka
      config:
        topic: "quanta-error-events"
        brokers: ["localhost:9094"]
sinks:
  - kafka
sink_configs:
  kafka:
    brokers: ["localhost:9094"]
    topic: "quanta-output"
    acks: all
dlq:
  enabled: true
  sink: kafka
  config:
    brokers: ["localhost:9094"]
    topic: "quanta-engine-dlq"
    acks: all
  include_original_headers: true
  include_error_metadata: true
```

This demonstrates all three error paths:
- **Plugin errors** → per-transformer `error_sink` (quanta-error-events topic)
- **Sink delivery failures** → engine DLQ (quanta-engine-dlq topic)
- **Transform infrastructure failures** → `DeadLetterFn` callback
