# Examples & Quickstarts

## File → Processor → Stdout

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
  - stdout
sink_configs:
  stdout:
    print_counter: true
    print_value: true
```

Run the transformer and engine:

```sh
UPPERCASE_LISTEN_ADDR=:50052 go run ./examples/transformers/uppercase &
QUANTA_PIPELINE_YML=pipeline.yml go run ./cmd/engine
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
docker compose -f test/kafkaenv/docker-compose.yaml up -d
UPPERCASE_LISTEN_ADDR=:50052 go run ./examples/transformers/uppercase &
QUANTA_PIPELINE_YML=pipeline.yml go run ./cmd/engine
```
