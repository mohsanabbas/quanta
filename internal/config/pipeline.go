package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

const SupportedPipelineSchema = "v1"

type PipelineConfig struct {
	SchemaVersion string              `yaml:"schema_version"`
	Source        SourceConfig        `yaml:"source"`
	Transformers  []TransformerConfig `yaml:"transformers"`
	Sinks         []string            `yaml:"sinks"`
	SinkConfigs   map[string]RawYAML  `yaml:"sink_configs"`
	Debug         DebugConfig         `yaml:"debug"`

	fileDir string `yaml:"-"`
}

type SourceConfig struct {
	Kind     string  `yaml:"kind"`
	Driver   string  `yaml:"driver"`
	Config   string  `yaml:"config"`
	Inline   RawYAML `yaml:"inline"`
	resolved string  `yaml:"-"`
}

func (s SourceConfig) ResolvedConfigPath() string {
	return s.resolved
}

type TransformerConfig struct {
	Name        string            `yaml:"name"`
	Type        string            `yaml:"type"`
	Address     string            `yaml:"address"`
	MaxInFlight int               `yaml:"max_in_flight"`
	TimeoutMS   int               `yaml:"timeout_ms"`
	ContentType string            `yaml:"content_type"`
	Retry       RetryPolicyConfig `yaml:"retry_policy"`
}

type RetryPolicyConfig struct {
	Attempts  int `yaml:"attempts"`
	BackoffMS int `yaml:"backoff_ms"`
}

type DebugConfig struct {
	PerFrameDelayMS int  `yaml:"per_frame_delay_ms"`
	PrintCounter    bool `yaml:"print_counter"`
	AckBatchSize    int  `yaml:"ack_batch_size"`
	AckFlushMS      int  `yaml:"ack_flush_ms"`
	PrintValue      bool `yaml:"print_value"`
	ValueMaxBytes   int  `yaml:"value_max_bytes"`
}

type RawYAML struct {
	Node *yaml.Node
}

func (r *RawYAML) UnmarshalYAML(value *yaml.Node) error {
	r.Node = value
	return nil
}

func (r *RawYAML) IsZero() bool {
	return r.Node == nil
}

func LoadPipelineSpec(path string) (PipelineConfig, error) {
	var cfg PipelineConfig
	raw, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return cfg, err
	}

	if cfg.SchemaVersion == "" {
		cfg.SchemaVersion = SupportedPipelineSchema
	}
	if cfg.SchemaVersion != SupportedPipelineSchema {
		return cfg, fmt.Errorf("pipeline schema_version %q not supported (want %q)", cfg.SchemaVersion, SupportedPipelineSchema)
	}

	if cfg.SinkConfigs == nil {
		cfg.SinkConfigs = map[string]RawYAML{}
	}
	cfg.fileDir = filepath.Dir(path)

	if err := cfg.validate(); err != nil {
		return cfg, err
	}

	if cfg.Source.Config != "" {
		cfg.Source.resolved = cfg.Source.Config
		if !filepath.IsAbs(cfg.Source.Config) {
			cfg.Source.resolved = filepath.Join(cfg.fileDir, cfg.Source.Config)
		}
	}

	return cfg, nil
}

func (c PipelineConfig) SinkConfig(name string) *yaml.Node {
	raw, ok := c.SinkConfigs[name]
	if !ok {
		return nil
	}
	return raw.Node
}

func (t TransformerConfig) Timeout() time.Duration {
	if t.TimeoutMS <= 0 {
		return 0
	}
	return time.Duration(t.TimeoutMS) * time.Millisecond
}

func (t TransformerConfig) RetryBackoff() time.Duration {
	if t.Retry.BackoffMS <= 0 {
		return 0
	}
	return time.Duration(t.Retry.BackoffMS) * time.Millisecond
}

func (c PipelineConfig) validate() error {
	if c.Source.Kind == "" {
		return fmt.Errorf("pipeline: source.kind required")
	}
	if c.Source.Driver == "" {
		return fmt.Errorf("pipeline: source.driver required")
	}
	if c.Source.Config == "" && c.Source.Inline.Node == nil {
		return fmt.Errorf("pipeline: source.config or source.inline required")
	}
	for i, t := range c.Transformers {
		if t.Name == "" {
			return fmt.Errorf("pipeline: transformers[%d].name required", i)
		}
		if t.Type == "" {
			return fmt.Errorf("pipeline: transformers[%d].type required", i)
		}
		switch t.Type {
		case "grpc":
			if t.Address == "" {
				return fmt.Errorf("pipeline: transformers[%d].address required for grpc", i)
			}
		default:
			return fmt.Errorf("pipeline: unsupported transformer type %q", t.Type)
		}
	}
	return nil
}
