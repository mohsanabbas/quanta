package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadPipelineSpec_ResolvesRelativeSourceConfigAndSchema(t *testing.T) {
	dir := t.TempDir()
	pipe := []byte(`schema_version: v1
source:
  kind: kafka
  driver: sarama
  config: kafka_source.yml
transformers: []
sinks: [stdout]
`)
	if err := os.WriteFile(filepath.Join(dir, "pipeline.yml"), pipe, 0o644); err != nil {
		t.Fatalf("write pipeline: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "kafka_source.yml"), []byte("schema_version: v1\n"), 0o644); err != nil {
		t.Fatalf("write kafka cfg: %v", err)
	}

	cfg, abs, err := LoadPipelineSpec(filepath.Join(dir, "pipeline.yml"))
	if err != nil {
		t.Fatalf("LoadPipelineSpec: %v", err)
	}
	if cfg.SchemaVersion != SupportedSchema {
		t.Fatalf("want schema %s, got %s", SupportedSchema, cfg.SchemaVersion)
	}
	if abs == "" || !filepath.IsAbs(abs) {
		t.Fatalf("want absolute kafka config path, got %q", abs)
	}
}

func TestLoadPipelineSpec_InvalidSchema(t *testing.T) {
	dir := t.TempDir()
	pipe := []byte(`schema_version: v999
source: { kind: kafka, driver: sarama, config: cf.yml }
transformers: []
sinks: [stdout]
`)
	if err := os.WriteFile(filepath.Join(dir, "pipeline.yml"), pipe, 0o644); err != nil {
		t.Fatalf("write pipeline: %v", err)
	}
	_, _, err := LoadPipelineSpec(filepath.Join(dir, "pipeline.yml"))
	if err == nil {
		t.Fatal("expected error for invalid schema_version")
	}
}
