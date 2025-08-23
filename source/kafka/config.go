package kafka

import (
	"errors"
	"fmt"
	"io/fs"
	"time"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

type CommitMode string

const (
	CommitAuto CommitMode = "auto" // like today
	CommitE2E  CommitMode = "e2e"  // wait for Ack
)

type BackPressureCfg struct {
	Capacity int64         `koanf:"capacity"`       // max unresolved frames
	CheckInt time.Duration `koanf:"check_interval"` // refill tick
}

type CheckpointCfg struct {
	CommitInt time.Duration `koanf:"commit_interval"` // flush cadence
}

type Config struct {
	Brokers   []string `koanf:"brokers"`
	Topics    []string `koanf:"topics"`
	GroupID   string   `koanf:"group_id"`
	StartFrom string   `koanf:"start_from"` // oldest|newest (default newest)
	Version   string   `koanf:"version"`
	TLSEn     bool     `koanf:"tls_enabled"`
	SASLUser  string   `koanf:"sasl_user"`
	SASLPass  string   `koanf:"sasl_pass"`

	CommitMode   CommitMode      `koanf:"commit_mode"` // auto|e2e
	BackPressure BackPressureCfg `koanf:"backpressure"`
	Checkpoint   CheckpointCfg   `koanf:"checkpoint"`
}

// ---------------------------------------------------------------------------
// Loader
// ---------------------------------------------------------------------------

// LoadConfig merges YAML (if present) with env-vars
// (prefix `QUANTA_KAFKA__`, delimiter `__`).
func LoadConfig(path string) (Config, error) {
	k := koanf.New(".")
	if path != "" {
		if err := k.Load(file.Provider(path), yaml.Parser()); err != nil &&
			!errors.Is(err, fs.ErrNotExist) {
			return Config{}, err
		}
	}
	// schema version check (only when YAML is present)
	sv := k.String("schema_version")
	if sv != "" && sv != "v1" {
		return Config{}, fmt.Errorf("kafka schema_version %q not supported (want v1)", sv)
	}

	_ = k.Load(env.Provider("QUANTA_KAFKA__", "__", nil), nil)

	var cfg Config
	if err := k.Unmarshal("", &cfg); err != nil {
		return cfg, err
	}
	if cfg.CommitMode == "" {
		cfg.CommitMode = CommitAuto
	}
	applyDefaults(&cfg)
	return cfg, nil
}

// ---------------------------------------------------------------------------
// defaults
// ---------------------------------------------------------------------------

func applyDefaults(c *Config) {
	if c.BackPressure.Capacity == 0 {
		c.BackPressure.Capacity = 30_000
	}
	if c.BackPressure.CheckInt == 0 {
		c.BackPressure.CheckInt = 100 * time.Millisecond
	}
	if c.Checkpoint.CommitInt == 0 {
		c.Checkpoint.CommitInt = 5 * time.Second
	}
	if c.CommitMode != CommitAuto && c.CommitMode != CommitE2E {
		c.CommitMode = CommitAuto
	}
	if c.StartFrom == "" {
		c.StartFrom = "newest"
	}

}
