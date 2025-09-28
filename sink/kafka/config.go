package kafka

import (
	"fmt"
	"time"
)

type Config struct {
	Brokers     []string `koanf:"brokers"`
	Topic       string   `koanf:"topic"`
	Version     string   `koanf:"version"`
	TLSEn       bool     `koanf:"tls_enabled"`
	SASLUser    string   `koanf:"sasl_user"`
	SASLPass    string   `koanf:"sasl_pass"`
	ClientID    string   `koanf:"client_id"`
	Acks        string   `koanf:"acks"`        // none|local|all
	Compression string   `koanf:"compression"` // none|gzip|snappy|lz4|zstd
	Idempotent  bool     `koanf:"idempotent"`

	// timeouts / retries
	Timeout         time.Duration `koanf:"timeout"`           // producer request timeout
	RetryMax        int           `koanf:"retry_max"`         // producer retry max
	RetryBackoffMin time.Duration `koanf:"retry_backoff_min"` // min backoff
	RetryBackoffMax time.Duration `koanf:"retry_backoff_max"` // max backoff

	// header-based overrides optional
	HeaderTopicKey string `koanf:"header_topic_key"` // e.g. "kafka.topic"
}

func (c *Config) validateAndDefault() error {
	if len(c.Brokers) == 0 {
		return fmt.Errorf("brokers required")
	}
	if c.Topic == "" && c.HeaderTopicKey == "" {
		return fmt.Errorf("either topic or header_topic_key must be set")
	}
	if c.Version == "" {
		c.Version = "3.6.0"
	}
	if c.Acks == "" {
		c.Acks = "all"
	}
	switch c.Acks {
	case "none", "local", "all":
	default:
		return fmt.Errorf("invalid acks %q (want: none|local|all)", c.Acks)
	}
	if c.Compression == "" {
		c.Compression = "none"
	}
	switch c.Compression {
	case "none", "gzip", "snappy", "lz4", "zstd":
	default:
		return fmt.Errorf("invalid compression %q", c.Compression)
	}
	if c.Timeout <= 0 {
		c.Timeout = 10 * time.Second
	}
	if c.RetryMax <= 0 {
		c.RetryMax = 3
	}
	if c.RetryBackoffMin <= 0 {
		c.RetryBackoffMin = 100 * time.Millisecond
	}
	if c.RetryBackoffMax < c.RetryBackoffMin {
		c.RetryBackoffMax = c.RetryBackoffMin
	}
	return nil
}
