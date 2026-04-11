package kafka

import (
	"errors"
	"time"

	qerr "quanta/internal/errors"
)

const (
	_defaultTimeout      = 10 * time.Second
	_defaultRetryMax     = 3
	_defaultRetryBackoff = 100 * time.Millisecond
	_defaultVersion      = "3.6.0"
	_acksAll             = "all"
	_acksNone            = "none"
	_acksLocal           = "local"
	_compressionNone     = "none"
)

type Config struct {
	Brokers     []string `koanf:"brokers"          yaml:"brokers"`
	Topic       string   `koanf:"topic"            yaml:"topic"`
	Version     string   `koanf:"version"          yaml:"version"`
	TLSEn       bool     `koanf:"tls_enabled"      yaml:"tls_enabled"`
	SASLUser    string   `koanf:"sasl_user"        yaml:"sasl_user"`
	SASLPass    string   `koanf:"sasl_pass"        yaml:"sasl_pass"`
	ClientID    string   `koanf:"client_id"        yaml:"client_id"`
	Acks        string   `koanf:"acks"             yaml:"acks"`
	Compression string   `koanf:"compression"      yaml:"compression"`
	Idempotent  bool     `koanf:"idempotent"       yaml:"idempotent"`

	Timeout         time.Duration `koanf:"timeout"           yaml:"timeout"`
	RetryMax        int           `koanf:"retry_max"         yaml:"retry_max"`
	RetryBackoffMin time.Duration `koanf:"retry_backoff_min" yaml:"retry_backoff_min"`
	RetryBackoffMax time.Duration `koanf:"retry_backoff_max" yaml:"retry_backoff_max"`

	HeaderTopicKey string `koanf:"header_topic_key" yaml:"header_topic_key"`
}

func (c *Config) validateAndDefault() error {
	if len(c.Brokers) == 0 {
		return qerr.Config("kafka-sink", "validate", errors.New("brokers required"))
	}
	if c.Topic == "" && c.HeaderTopicKey == "" {
		return qerr.Config("kafka-sink", "validate", errors.New("either topic or header_topic_key must be set"))
	}
	if c.Version == "" {
		c.Version = _defaultVersion
	}
	if c.Acks == "" {
		c.Acks = _acksAll
	}
	switch c.Acks {
	case _acksNone, _acksLocal, _acksAll:
	default:
		return qerr.Config("kafka-sink", "validate", errors.New("unsupported acks value"))
	}
	if c.Compression == "" {
		c.Compression = _compressionNone
	}
	switch c.Compression {
	case "none", "gzip", "snappy", "lz4", "zstd":
	default:
		return qerr.Config("kafka-sink", "validate", errors.New("unsupported compression"))
	}
	if c.Timeout <= 0 {
		c.Timeout = _defaultTimeout
	}
	if c.RetryMax <= 0 {
		c.RetryMax = _defaultRetryMax
	}
	if c.RetryBackoffMin <= 0 {
		c.RetryBackoffMin = _defaultRetryBackoff
	}
	if c.RetryBackoffMax < c.RetryBackoffMin {
		c.RetryBackoffMax = c.RetryBackoffMin
	}
	return nil
}
