-- ClickHouse initialization script
-- Creates database and table for AI events from Quanta pipeline

CREATE DATABASE IF NOT EXISTS analytics;

CREATE TABLE IF NOT EXISTS analytics.ai_events (
    -- CloudEvents metadata
    event_id String,
    event_source String,
    event_type String,
    event_time DateTime64(6, 'UTC'),
    
    -- AI request fields
    provider LowCardinality(String),
    model String,
    request_id String,
    
    -- Token usage
    input_tokens Int64,
    output_tokens Int64,
    total_tokens Int64,
    
    -- Performance
    latency_ms Int64,
    
    -- Status
    status LowCardinality(String),
    status_class LowCardinality(String),
    
    -- Request config
    temperature Float64,
    stream_enabled UInt8,
    
    -- Context
    environment LowCardinality(String),
    org_id String,
    user_id String
) ENGINE = MergeTree()
ORDER BY (event_time, event_id)
PARTITION BY toYYYYMM(event_time)
SETTINGS index_granularity = 8192;

-- Create materialized view for real-time aggregations
CREATE TABLE IF NOT EXISTS analytics.ai_events_hourly (
    hour DateTime,
    provider LowCardinality(String),
    model String,
    status LowCardinality(String),
    request_count UInt64,
    total_input_tokens UInt64,
    total_output_tokens UInt64,
    avg_latency_ms Float64,
    p99_latency_ms Float64
) ENGINE = SummingMergeTree()
ORDER BY (hour, provider, model, status)
PARTITION BY toYYYYMM(hour);

CREATE MATERIALIZED VIEW IF NOT EXISTS analytics.ai_events_hourly_mv
TO analytics.ai_events_hourly
AS SELECT
    toStartOfHour(event_time) AS hour,
    provider,
    model,
    status,
    count() AS request_count,
    sum(input_tokens) AS total_input_tokens,
    sum(output_tokens) AS total_output_tokens,
    avg(latency_ms) AS avg_latency_ms,
    quantile(0.99)(latency_ms) AS p99_latency_ms
FROM analytics.ai_events
GROUP BY hour, provider, model, status;
