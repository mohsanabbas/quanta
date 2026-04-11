package kafka

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	qerr "quanta/internal/errors"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

type CommitMode string

const (
	CommitAuto CommitMode = "auto"
	CommitE2E  CommitMode = "e2e"

	_defaultMaxBytes       = 100 << 20
	_defaultWindow         = 4096
	_defaultCommitInterval = 5 * time.Second
	_defaultCommitStep     = 128
	_minWindowBits         = 256
)

type PublicConfig struct {
	Brokers              []string   `koanf:"brokers"`
	Topics               []string   `koanf:"topics"`
	GroupID              string     `koanf:"group_id"`
	StartFrom            string     `koanf:"start_from"`
	Version              string     `koanf:"version"`
	TLSEn                bool       `koanf:"tls_enabled"`
	SASLUser             string     `koanf:"sasl_user"`
	SASLPass             string     `koanf:"sasl_pass"`
	CommitMode           CommitMode `koanf:"commit_mode"`
	SaramaVerbose        bool       `koanf:"sarama_verbose"`
	BackpressureStrategy string     `koanf:"backpressure_strategy"`
	CheckpointStrategy   string     `koanf:"checkpoint_strategy"`
	CommitStrategyType   string     `koanf:"commit_strategy_type"`
}

type Tuning struct {
	InFlightBytes  int64         `koanf:"inflight_bytes"`
	InFlightMsgs   int64         `koanf:"inflight_msgs"`
	WindowBits     uint32        `koanf:"window_bits"`
	CommitInterval time.Duration `koanf:"commit_interval"`
	CommitStep     uint32        `koanf:"commit_step"`
}

type Config struct {
	public PublicConfig
	tuning Tuning
}

func (c Config) Public() PublicConfig {
	return c.public
}

func (c Config) Tuning() Tuning {
	return c.tuning
}

func LoadConfig(path string) (Config, error) {
	var cfg Config
	public, err := loadPublicConfig(path)
	if err != nil {
		return cfg, err
	}
	tuning, err := loadTuningConfig(path)
	if err != nil {
		return cfg, err
	}
	cfg.public = public
	cfg.tuning = tuning
	return cfg, nil
}

func loadPublicConfig(path string) (PublicConfig, error) {
	k := koanf.New(".")
	if path != "" {
		if err := k.Load(file.Provider(path), yaml.Parser()); err != nil {
			return PublicConfig{}, err
		}
	}

	if err := k.Load(env.Provider("QUANTA_SOURCE__", "__", publicEnvKey), nil); err != nil {
		return PublicConfig{}, err
	}
	var public PublicConfig
	if err := k.Unmarshal("", &public); err != nil {
		return public, err
	}
	applyPublicDefaults(&public)
	if err := validatePublic(public); err != nil {
		return public, err
	}
	return public, nil
}

func loadTuningConfig(publicPath string) (Tuning, error) {
	k := koanf.New(".")
	if publicPath != "" {
		if tuningPath := deriveTuningPath(publicPath); tuningPath != "" {
			if _, err := os.Stat(tuningPath); err == nil {
				if err := k.Load(file.Provider(tuningPath), yaml.Parser()); err != nil {
					return Tuning{}, err
				}
			} else if !errors.Is(err, os.ErrNotExist) {
				return Tuning{}, err
			}
		}
	}

	if err := k.Load(env.Provider("QUANTA_TUNING__", "__", tuningEnvKey), nil); err != nil {
		return Tuning{}, err
	}
	var tuning Tuning
	if err := k.Unmarshal("", &tuning); err != nil {
		return tuning, err
	}
	applyTuningDefaults(&tuning)
	if err := validateTuning(tuning); err != nil {
		return tuning, err
	}
	return tuning, nil
}

func deriveTuningPath(publicPath string) string {
	if publicPath == "" {
		return ""
	}
	ext := filepath.Ext(publicPath)
	base := strings.TrimSuffix(publicPath, ext)
	if ext == "" {
		return base + ".tuning"
	}
	return base + ".tuning" + ext
}

func publicEnvKey(key string) string {
	key = strings.TrimPrefix(key, "QUANTA_SOURCE__")
	key = strings.ReplaceAll(key, "__", ".")
	return strings.ToLower(key)
}

func tuningEnvKey(key string) string {
	key = strings.TrimPrefix(key, "QUANTA_TUNING__")
	key = strings.ReplaceAll(key, "__", ".")
	return strings.ToLower(key)
}

func applyPublicDefaults(c *PublicConfig) {
	if c.CommitMode == "" {
		c.CommitMode = CommitAuto
	}
	c.CommitMode = CommitMode(strings.ToLower(string(c.CommitMode)))
	if c.StartFrom == "" {
		c.StartFrom = "newest"
	}
	c.StartFrom = strings.ToLower(c.StartFrom)
	if c.BackpressureStrategy == "" {
		c.BackpressureStrategy = "combined"
	}
	c.BackpressureStrategy = strings.ToLower(c.BackpressureStrategy)
	if c.CheckpointStrategy == "" {
		c.CheckpointStrategy = "sliding_window"
	}
	c.CheckpointStrategy = strings.ToLower(c.CheckpointStrategy)
	if c.CommitStrategyType == "" {
		c.CommitStrategyType = "hybrid"
	}
	c.CommitStrategyType = strings.ToLower(c.CommitStrategyType)
}

func validatePublic(c PublicConfig) error {
	if len(c.Brokers) == 0 {
		return qerr.Config("kafka", "validate", errors.New("brokers required"))
	}
	if len(c.Topics) == 0 {
		return qerr.Config("kafka", "validate", errors.New("topics required"))
	}
	if c.GroupID == "" {
		return qerr.Config("kafka", "validate", errors.New("group_id required"))
	}
	switch c.CommitMode {
	case CommitAuto, CommitE2E:
	default:
		return qerr.Config("kafka", "validate", errors.New("unsupported commit_mode"))
	}
	switch c.StartFrom {
	case "oldest", "newest":
	default:
		return qerr.Config("kafka", "validate", errors.New("unsupported start_from"))
	}
	return nil
}

func applyTuningDefaults(t *Tuning) {
	if t.InFlightBytes <= 0 {
		t.InFlightBytes = _defaultMaxBytes
	}
	if t.InFlightMsgs <= 0 {
		t.InFlightMsgs = _defaultWindow
	}
	if t.WindowBits == 0 {
		t.WindowBits = _defaultWindow
	}
	if t.CommitInterval <= 0 {
		t.CommitInterval = _defaultCommitInterval
	}
	if t.CommitStep == 0 {
		t.CommitStep = _defaultCommitStep
	}
}

func validateTuning(t Tuning) error {
	if t.InFlightBytes <= 0 {
		return qerr.Config("kafka", "validate", errors.New("inflight_bytes must be positive"))
	}
	if t.InFlightMsgs <= 0 {
		return qerr.Config("kafka", "validate", errors.New("inflight_msgs must be positive"))
	}
	if t.WindowBits < _minWindowBits {
		return qerr.Config("kafka", "validate", errors.New("window_bits must be >= 256"))
	}
	if int64(t.WindowBits) < t.InFlightMsgs {
		return qerr.Config("kafka", "validate", errors.New("inflight_msgs must be <= window_bits"))
	}
	if t.CommitInterval <= 0 {
		return qerr.Config("kafka", "validate", errors.New("commit_interval must be positive"))
	}
	if t.CommitStep == 0 {
		return qerr.Config("kafka", "validate", errors.New("commit_step must be positive"))
	}
	return nil
}
