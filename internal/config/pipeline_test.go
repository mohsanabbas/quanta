package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadPipelineSpec(t *testing.T) {
	dir := t.TempDir()
	pipe := []byte(`schema_version: v1
source:
  kind: kafka
  driver: sarama
  config: kafka_source.yml
transformers:
  - name: uppercase
    type: grpc
    address: localhost:50052
    timeout_ms: 750
    retry_policy:
      attempts: 5
      backoff_ms: 120
sinks: [stdout, kafka]
sink_configs:
  stdout: {}
  kafka:
    brokers: ["localhost:9092"]
    topic: example
`)
	if err := os.WriteFile(filepath.Join(dir, "pipeline.yml"), pipe, 0o600); err != nil {
		t.Fatalf("write pipeline: %v", err)
	}
	kafkaCfg := []byte("schema_version: v1\n")
	if err := os.WriteFile(filepath.Join(dir, "kafka_source.yml"), kafkaCfg, 0o600); err != nil {
		t.Fatalf("write kafka cfg: %v", err)
	}

	cfg, err := LoadPipelineSpec(filepath.Join(dir, "pipeline.yml"))
	if err != nil {
		t.Fatalf("LoadPipelineSpec: %v", err)
	}

	if cfg.SchemaVersion != SupportedPipelineSchema {
		t.Fatalf("schema mismatch: want %s got %s", SupportedPipelineSchema, cfg.SchemaVersion)
	}
	if cfg.Source.Kind != "kafka" || cfg.Source.Driver != "sarama" {
		t.Fatalf("unexpected source: %+v", cfg.Source)
	}
	if cfg.Source.ResolvedConfigPath() == "" || !filepath.IsAbs(cfg.Source.ResolvedConfigPath()) {
		t.Fatalf("source config path not absolute: %q", cfg.Source.ResolvedConfigPath())
	}
	if len(cfg.Transformers) != 1 {
		t.Fatalf("expected 1 transformer, got %d", len(cfg.Transformers))
	}
	tr := cfg.Transformers[0]
	if tr.Timeout().Milliseconds() != 750 {
		t.Fatalf("timeout mismatch: %v", tr.Timeout())
	}
	if tr.Retry.Attempts != 5 || tr.RetryBackoff().Milliseconds() != 120 {
		t.Fatalf("retry mismatch: %+v", tr.Retry)
	}
	if cfg.SinkConfig("stdout") == nil {
		t.Fatalf("stdout sink config missing")
	}
	if cfg.SinkConfig("kafka") == nil {
		t.Fatalf("kafka sink config missing")
	}
}

func TestLoadPipelineSpec_InvalidSchema(t *testing.T) {
	dir := t.TempDir()
	pipe := []byte(`schema_version: v999
source: { kind: kafka, driver: sarama, config: cf.yml }
transformers: []
sinks: [stdout]
`)
	if err := os.WriteFile(filepath.Join(dir, "pipeline.yml"), pipe, 0o600); err != nil {
		t.Fatalf("write pipeline: %v", err)
	}
	if _, err := LoadPipelineSpec(filepath.Join(dir, "pipeline.yml")); err == nil {
		t.Fatal("expected error for invalid schema_version")
	}
}

func TestLoadPipelineSpec_InvalidSource(t *testing.T) {
	dir := t.TempDir()
	pipe := []byte(`schema_version: v1
source:
  kind: kafka
  driver: ""
  config: cf.yml
transformers: []
sinks: []
`)
	if err := os.WriteFile(filepath.Join(dir, "pipeline.yml"), pipe, 0o600); err != nil {
		t.Fatalf("write pipeline: %v", err)
	}
	if _, err := LoadPipelineSpec(filepath.Join(dir, "pipeline.yml")); err == nil {
		t.Fatal("expected validation error for missing driver")
	}
}
