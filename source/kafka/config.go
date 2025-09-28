package kafka

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

// CommitMode defines how offsets are committed for the Kafka source. When set to
// CommitAuto the driver will mark messages as delivered immediately after
// emitting them downstream. When set to CommitE2E the driver will wait until
// an ack is received from the pipeline before committing the offset.
type CommitMode string

const (
	// CommitAuto commits offsets as soon as the message has been emitted to the
	// pipeline. This is the default mode and trades exactly‑once delivery for
	// throughput.
	CommitAuto CommitMode = "auto"
	// CommitE2E waits for an acknowledgment from the pipeline before
	// committing the offset. This provides at‑least‑once semantics across the
	// entire pipeline.
	CommitE2E CommitMode = "e2e"
)

// PublicConfig holds user‑visible configuration for the Kafka source driver.
// These values are loaded from YAML and environment variables and control
// functional behaviour such as which brokers to connect to and which topics
// to consume.
type PublicConfig struct {
	Brokers       []string   `koanf:"brokers"`
	Topics        []string   `koanf:"topics"`
	GroupID       string     `koanf:"group_id"`
	StartFrom     string     `koanf:"start_from"`
	Version       string     `koanf:"version"`
	TLSEn         bool       `koanf:"tls_enabled"`
	SASLUser      string     `koanf:"sasl_user"`
	SASLPass      string     `koanf:"sasl_pass"`
	CommitMode    CommitMode `koanf:"commit_mode"`
	SaramaVerbose bool       `koanf:"sarama_verbose"`
}

// Tuning contains internal knobs for the Kafka source driver. These values are
// not intended to be surfaced to end users directly and primarily control
// backpressure and checkpoint behaviour. A separate YAML file with the same
// base name and a `.tuning` suffix may override these values.
type Tuning struct {
	InFlightBytes  int64         `koanf:"inflight_bytes"`
	InFlightMsgs   int64         `koanf:"inflight_msgs"`
	WindowBits     uint32        `koanf:"window_bits"`
	CommitInterval time.Duration `koanf:"commit_interval"`
	CommitStep     uint32        `koanf:"commit_step"`
}

// Config groups the public configuration and tuning parameters together. This
// type is passed into the Kafka driver on Configure() calls.
type Config struct {
	Public PublicConfig
	Tuning Tuning
}

// LoadConfig loads the public and tuning configuration from the provided
// filesystem path. The public configuration is loaded from the file exactly as
// specified. If a sibling file with the same name but a `.tuning` suffix
// exists it will be loaded for the tuning configuration. Environment variables
// prefixed with `QUANTA_SOURCE__` and `QUANTA_TUNING__` override values in
// the respective sections.
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
	cfg.Public = public
	cfg.Tuning = tuning
	return cfg, nil
}

func loadPublicConfig(path string) (PublicConfig, error) {
	k := koanf.New(".")
	if path != "" {
		if err := k.Load(file.Provider(path), yaml.Parser()); err != nil {
			return PublicConfig{}, err
		}
	}
	// Environment variables override file values. Keys are mapped from
	// QUANTA_SOURCE__FOO__BAR to foo.bar.
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
	// If a tuning file exists alongside the public config, load it.
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
	// Environment variables override file values. Keys are mapped from
	// QUANTA_TUNING__FOO__BAR to foo.bar.
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

// deriveTuningPath constructs the path to the tuning configuration by
// inserting ".tuning" before the file extension. For example,
// config.yaml → config.tuning.yaml.
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

// publicEnvKey transforms QUANTA_SOURCE__FOO__BAR into foo.bar for the koanf
// environment provider.
func publicEnvKey(key string) string {
	key = strings.TrimPrefix(key, "QUANTA_SOURCE__")
	key = strings.ReplaceAll(key, "__", ".")
	return strings.ToLower(key)
}

// tuningEnvKey transforms QUANTA_TUNING__FOO__BAR into foo.bar for the koanf
// environment provider.
func tuningEnvKey(key string) string {
	key = strings.TrimPrefix(key, "QUANTA_TUNING__")
	key = strings.ReplaceAll(key, "__", ".")
	return strings.ToLower(key)
}

// applyPublicDefaults sets default values on the public configuration when
// fields are missing. CommitMode defaults to auto and StartFrom defaults to
// newest. The CommitMode and StartFrom values are also normalized to lower
// case.
func applyPublicDefaults(c *PublicConfig) {
	if c.CommitMode == "" {
		c.CommitMode = CommitAuto
	}
	c.CommitMode = CommitMode(strings.ToLower(string(c.CommitMode)))
	if c.StartFrom == "" {
		c.StartFrom = "newest"
	}
	c.StartFrom = strings.ToLower(c.StartFrom)
}

// validatePublic checks that required fields are present and values are
// supported. It returns an error if any validation fails.
func validatePublic(c PublicConfig) error {
	if len(c.Brokers) == 0 {
		return errors.New("kafka: brokers required")
	}
	if len(c.Topics) == 0 {
		return errors.New("kafka: topics required")
	}
	if c.GroupID == "" {
		return errors.New("kafka: group_id required")
	}
	switch c.CommitMode {
	case CommitAuto, CommitE2E:
	default:
		return fmt.Errorf("kafka: invalid commit_mode %q", c.CommitMode)
	}
	switch c.StartFrom {
	case "oldest", "newest":
	default:
		return fmt.Errorf("kafka: invalid start_from %q", c.StartFrom)
	}
	return nil
}

// applyTuningDefaults sets sensible defaults for tuning parameters when
// unspecified. It ensures the window size is at least equal to the in‑flight
// message count and applies default commit intervals and steps.
func applyTuningDefaults(t *Tuning) {
	if t.InFlightBytes <= 0 {
		t.InFlightBytes = 256 << 20 // 256 MiB
	}
	if t.InFlightMsgs <= 0 {
		t.InFlightMsgs = 4_096
	}
	if t.WindowBits == 0 {
		t.WindowBits = 4096
	}
	// Ensure the window is at least as large as the in‑flight message count.
	if int64(t.WindowBits) < t.InFlightMsgs {
		t.InFlightMsgs = int64(t.WindowBits)
	}
	if t.CommitInterval <= 0 {
		t.CommitInterval = 5 * time.Second
	}
	if t.CommitStep == 0 {
		t.CommitStep = 128
	}
}

// validateTuning verifies that tuning values are within acceptable ranges.
// inflight values must be positive, window size must be >= 256 and at least
// inflight_msgs, commit_interval and commit_step must be positive.
func validateTuning(t Tuning) error {
	if t.InFlightBytes <= 0 {
		return errors.New("kafka tuning: inflight_bytes must be positive")
	}
	if t.InFlightMsgs <= 0 {
		return errors.New("kafka tuning: inflight_msgs must be positive")
	}
	if t.WindowBits < 256 {
		return errors.New("kafka tuning: window_bits must be >= 256")
	}
	if int64(t.WindowBits) < t.InFlightMsgs {
		return fmt.Errorf("kafka tuning: inflight_msgs (%d) must be <= window_bits (%d)", t.InFlightMsgs, t.WindowBits)
	}
	if t.CommitInterval <= 0 {
		return errors.New("kafka tuning: commit_interval must be positive")
	}
	if t.CommitStep == 0 {
		return errors.New("kafka tuning: commit_step must be positive")
	}
	return nil
}
