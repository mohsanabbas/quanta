package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadPipelineSpec_DLQConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		giveDLQ         string
		wantEnabled     bool
		wantSink        string
		wantHeaders     bool
		wantErrMetadata bool
	}{
		{
			name: "dlq_enabled_full",
			giveDLQ: `
dlq:
  enabled: true
  sink: kafka
  config:
    topic: quanta-dlq
    brokers: ["localhost:9092"]
  include_original_headers: true
  include_error_metadata: true`,
			wantEnabled:     true,
			wantSink:        "kafka",
			wantHeaders:     true,
			wantErrMetadata: true,
		},
		{
			name: "dlq_disabled_explicit",
			giveDLQ: `
dlq:
  enabled: false
  sink: kafka`,
			wantEnabled: false,
			wantSink:    "kafka",
		},
		{
			name:        "dlq_absent",
			giveDLQ:     "",
			wantEnabled: false,
		},
		{
			name: "dlq_enabled_stdout",
			giveDLQ: `
dlq:
  enabled: true
  sink: stdout`,
			wantEnabled: true,
			wantSink:    "stdout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			pipe := `schema_version: v1
source:
  kind: kafka
  driver: sarama
  config: kafka_source.yml
transformers: []
sinks: [stdout]
` + tt.giveDLQ

			if err := os.WriteFile(filepath.Join(dir, "pipeline.yml"), []byte(pipe), 0o600); err != nil {
				t.Fatalf("write pipeline: %v", err)
			}
			if err := os.WriteFile(filepath.Join(dir, "kafka_source.yml"), []byte("schema_version: v1\n"), 0o600); err != nil {
				t.Fatalf("write kafka cfg: %v", err)
			}

			cfg, err := LoadPipelineSpec(filepath.Join(dir, "pipeline.yml"))
			if err != nil {
				t.Fatalf("LoadPipelineSpec: %v", err)
			}

			if cfg.DLQ == nil {
				if tt.wantEnabled {
					t.Fatal("DLQ config is nil, but expected enabled")
				}
				return
			}

			if cfg.DLQ.Enabled != tt.wantEnabled {
				t.Fatalf("DLQ.Enabled: got %v, want %v", cfg.DLQ.Enabled, tt.wantEnabled)
			}
			if cfg.DLQ.Sink != tt.wantSink {
				t.Fatalf("DLQ.Sink: got %q, want %q", cfg.DLQ.Sink, tt.wantSink)
			}
			if cfg.DLQ.IncludeOriginalHeaders != tt.wantHeaders {
				t.Fatalf("DLQ.IncludeOriginalHeaders: got %v, want %v", cfg.DLQ.IncludeOriginalHeaders, tt.wantHeaders)
			}
			if cfg.DLQ.IncludeErrorMetadata != tt.wantErrMetadata {
				t.Fatalf("DLQ.IncludeErrorMetadata: got %v, want %v", cfg.DLQ.IncludeErrorMetadata, tt.wantErrMetadata)
			}
		})
	}
}

func TestLoadPipelineSpec_DLQValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		giveDLQ string
		wantErr bool
	}{
		{
			name: "enabled_without_sink",
			giveDLQ: `
dlq:
  enabled: true`,
			wantErr: true,
		},
		{
			name: "disabled_without_sink_ok",
			giveDLQ: `
dlq:
  enabled: false`,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			pipe := `schema_version: v1
source:
  kind: kafka
  driver: sarama
  config: kafka_source.yml
transformers: []
sinks: [stdout]
` + tt.giveDLQ

			if err := os.WriteFile(filepath.Join(dir, "pipeline.yml"), []byte(pipe), 0o600); err != nil {
				t.Fatalf("write pipeline: %v", err)
			}
			if err := os.WriteFile(filepath.Join(dir, "kafka_source.yml"), []byte("schema_version: v1\n"), 0o600); err != nil {
				t.Fatalf("write kafka cfg: %v", err)
			}

			_, err := LoadPipelineSpec(filepath.Join(dir, "pipeline.yml"))
			if tt.wantErr && err == nil {
				t.Fatal("expected validation error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
