package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"quanta/internal/spec"
)

const SupportedSchema = "v1"

// LoadPipelineSpec parses a pipeline YAML, validates schema_version, and
// returns the parsed spec and an absolute path to the source config (if set).
func LoadPipelineSpec(path string) (spec.File, string, error) {
	var cfg spec.File
	raw, err := os.ReadFile(path)
	if err != nil {
		return cfg, "", err
	}
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return cfg, "", err
	}
	if cfg.SchemaVersion == "" {
		cfg.SchemaVersion = SupportedSchema
	}
	if cfg.SchemaVersion != SupportedSchema {
		return cfg, "", fmt.Errorf("pipeline schema_version %q not supported (want %q)", cfg.SchemaVersion, SupportedSchema)
	}
	confPath := cfg.Source.Config
	if confPath != "" && !filepath.IsAbs(confPath) {
		confPath = filepath.Join(filepath.Dir(path), confPath)
	}
	return cfg, confPath, nil
}
