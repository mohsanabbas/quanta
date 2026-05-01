# Examples & Quickstarts

## Kafka → Transformer → Stdout

Basic debugging setup:

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

```bash
go run ./examples/transformers/uppercase --listen=:50052 &
go run ./cmd/engine
```

---

## Kafka → Kafka + DLQ

Production pattern with dead-letter queue:

```yaml
schema_version: v1
source:
  kind: kafka
  driver: sarama
  config: kafka_source.yml
transformers:
  - name: cloudevents
    type: grpc
    address: "transformer:50052"
    error_sink:
      sink: kafka
      config:
        topic: "quanta-error-events"
        brokers: ["kafka:29092"]
sinks:
  - kafka
sink_configs:
  kafka:
    brokers: ["kafka:29092"]
    topic: "quanta-output"
    acks: all
dlq:
  enabled: true
  sink: kafka
  config:
    brokers: ["kafka:29092"]
    topic: "quanta-engine-dlq"
```

---

## Kafka → S3 (JSONL)

Simple data lake archival:

```yaml
schema_version: v1
source:
  kind: kafka
  driver: sarama
  config: kafka_source.yml
sinks:
  - s3
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

---

## Kafka → S3 (Parquet)

Analytics-ready Parquet with schema mapping:

```yaml
schema_version: v1
source:
  kind: kafka
  driver: sarama
  config: kafka_source.yml
sinks:
  - s3
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

Query with Athena/Spark:
```sql
SELECT provider, model, sum(total_tokens) 
FROM quanta_events 
WHERE event_time > current_date - interval '1' day
GROUP BY provider, model;
```

---

## Kafka → ClickHouse

Real-time OLAP analytics:

```yaml
schema_version: v1
source:
  kind: kafka
  driver: sarama
  config: kafka_source.yml
sinks:
  - clickhouse
sink_configs:
  clickhouse:
    host: "clickhouse:9000"
    database: analytics
    table: ai_events
    schema_file: topology/schemas/ai_events.schema.yaml
    
    auth_strategy: native
    username: default
    password_env: CLICKHOUSE_PASSWORD
    
    batch_size: 5000
    flush_interval: 5s
    compression: lz4
```

Query immediately:
```sql
SELECT 
    provider,
    model,
    count() as requests,
    sum(total_tokens) as tokens,
    avg(latency_ms) as avg_latency
FROM analytics.ai_events
WHERE event_time > now() - INTERVAL 1 HOUR
GROUP BY provider, model
ORDER BY requests DESC;
```

---

## Kafka → Multi-Sink (S3 + ClickHouse)

Fan-out to data lake and OLAP:

```yaml
schema_version: v1
source:
  kind: kafka
  driver: sarama
  config: kafka_source.yml
  
schemas:
  - schemas/ai_events.schema.yaml
  
sinks:
  - s3
  - clickhouse
  
sink_configs:
  s3:
    bucket: quanta-archive
    region: us-east-1
    prefix: raw/
    format: parquet
    schema_file: topology/schemas/ai_events.schema.yaml
    batch_size: 5000
    flush_interval: 30s
    auth_strategy: iam-role
    
  clickhouse:
    host: "clickhouse:9000"
    database: analytics
    table: ai_events
    schema_file: topology/schemas/ai_events.schema.yaml
    auth_strategy: native
    username: default
    password_env: CLICKHOUSE_PASSWORD
    batch_size: 5000
    flush_interval: 5s
    compression: lz4

dlq:
  enabled: true
  sink: kafka
  config:
    brokers: ["kafka:29092"]
    topic: "quanta-dlq"
```

---

## Docker Development Stack

Complete local development:

```bash
# Start infrastructure
docker compose up -d kafka clickhouse localstack

# Start engine
make docker-up ARCH=arm64

# Access UIs
open http://localhost:8080      # Kafka UI
open http://localhost:8082      # S3 Manager
open http://localhost:8123/play # ClickHouse Play

# Verify ClickHouse
curl 'http://localhost:8123/?query=SELECT%20count()%20FROM%20analytics.ai_events'

# Verify S3
aws --endpoint-url=http://localhost:4566 s3 ls s3://quanta-output/events/
```

---

## Error Paths

Three-path error handling demonstration:

| Path | Trigger | Destination |
|------|---------|-------------|
| Plugin errors | Transformer returns ERROR | `error_sink` topic |
| Sink delivery failure | S3/ClickHouse write fails | Engine DLQ |
| Infrastructure failure | gRPC timeout, connection lost | `DeadLetterFn` → DLQ |
