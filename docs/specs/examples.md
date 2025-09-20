# Examples & Quickstarts

## Minimal Pipeline: File → Transformer → Kafka Sink

```yaml
schema_version: v1
source:
  kind: file
  driver: local
  config: file_source.yml
transformers:
  - name: uppercase
    type: grpc
    address: "localhost:50052"
sinks:
  - kafka
sink_configs:
  kafka:
    brokers: ["kafka:29092"]
    topic: "output"
```

## Kafka → Transform → Kafka

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
  - stdout
sink_configs:
  kafka:
    brokers: ["kafka:29092"]
    topic: "normalized.events"
  stdout:
    print_counter: true
    print_value: true
```

### Running (local)

```sh
make build
UPPERCASE_LISTEN_ADDR=:50052 go run ./examples/transformers/uppercase &
QUANTA_PIPELINE_YML=pipeline.yml go run ./cmd/engine
```

