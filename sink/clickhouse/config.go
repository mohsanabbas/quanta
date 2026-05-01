package clickhouse

import (
	"errors"
	"fmt"
	"os"
	"time"

	qerr "quanta/internal/errors"
)

// Config defaults.
const (
	defaultBatchSize     = 10000
	defaultFlushInterval = 5 * time.Second
	defaultDialTimeout   = 10 * time.Second
	defaultMaxIdleConns  = 5
	defaultMaxOpenConns  = 10
	defaultConnMaxLife   = time.Hour
)

// Environment variable names for AuthEnv strategy.
const (
	envUsername = "CLICKHOUSE_USER"
	envPassword = "CLICKHOUSE_PASSWORD"
)

// Config holds ClickHouse sink configuration.
type Config struct {
	// Connection
	Host     string   `yaml:"host"`
	Hosts    []string `yaml:"hosts"`
	Database string   `yaml:"database"`
	Table    string   `yaml:"table"`

	// Authentication
	AuthStrategy AuthStrategy `yaml:"auth_strategy"`
	Username     string       `yaml:"username"`
	Password     string       `yaml:"password"`
	UsernameEnv  string       `yaml:"username_env"`
	PasswordEnv  string       `yaml:"password_env"`

	// TLS
	TLS         bool   `yaml:"tls"`
	TLSInsecure bool   `yaml:"tls_insecure"`
	CACert      string `yaml:"ca_cert"`
	ClientCert  string `yaml:"client_cert"`
	ClientKey   string `yaml:"client_key"`

	// Schema
	SchemaFile string `yaml:"schema_file"`

	// Batching
	BatchSize     int           `yaml:"batch_size"`
	FlushInterval time.Duration `yaml:"flush_interval"`

	// Connection pool
	DialTimeout  time.Duration `yaml:"dial_timeout"`
	MaxIdleConns int           `yaml:"max_idle_conns"`
	MaxOpenConns int           `yaml:"max_open_conns"`
	ConnMaxLife  time.Duration `yaml:"conn_max_lifetime"`

	// Compression
	Compression Compression `yaml:"compression"`
}

func (c *Config) validate() error {
	if c.Host == "" && len(c.Hosts) == 0 {
		return qerr.Config("clickhouse", "validate", errors.New("host or hosts required"))
	}
	if c.Database == "" {
		return qerr.Config("clickhouse", "validate", errors.New("database required"))
	}
	if c.Table == "" {
		return qerr.Config("clickhouse", "validate", errors.New("table required"))
	}
	if c.SchemaFile == "" {
		return qerr.Config("clickhouse", "validate", errors.New("schema_file required"))
	}

	c.applyDefaults()
	return c.validateAuth()
}

func (c *Config) applyDefaults() {
	if c.BatchSize <= 0 {
		c.BatchSize = defaultBatchSize
	}
	if c.FlushInterval <= 0 {
		c.FlushInterval = defaultFlushInterval
	}
	if c.DialTimeout <= 0 {
		c.DialTimeout = defaultDialTimeout
	}
	if c.MaxIdleConns <= 0 {
		c.MaxIdleConns = defaultMaxIdleConns
	}
	if c.MaxOpenConns <= 0 {
		c.MaxOpenConns = defaultMaxOpenConns
	}
	if c.ConnMaxLife <= 0 {
		c.ConnMaxLife = defaultConnMaxLife
	}
	if c.AuthStrategy == "" {
		c.AuthStrategy = AuthNative
	}
	if c.Compression == "" {
		c.Compression = CompressionLZ4
	}
}

func (c *Config) validateAuth() error {
	switch c.AuthStrategy {
	case AuthNative:
		if c.resolveUsername() == "" {
			return qerr.Config("clickhouse", "validate", errors.New("username required for native auth"))
		}
	case AuthTLS:
		if c.ClientCert == "" || c.ClientKey == "" {
			return qerr.Config("clickhouse", "validate", errors.New("client_cert and client_key required for tls auth"))
		}
		c.TLS = true // Force TLS for mTLS auth
	case AuthEnv:
		// Validated at connection time
	default:
		return qerr.Config("clickhouse", "validate", fmt.Errorf("unsupported auth_strategy: %s", c.AuthStrategy))
	}
	return nil
}

func (c *Config) resolveUsername() string {
	if c.UsernameEnv != "" {
		if v := os.Getenv(c.UsernameEnv); v != "" {
			return v
		}
	}
	if c.AuthStrategy == AuthEnv {
		if v := os.Getenv(envUsername); v != "" {
			return v
		}
	}
	return c.Username
}

func (c *Config) resolvePassword() string {
	if c.PasswordEnv != "" {
		if v := os.Getenv(c.PasswordEnv); v != "" {
			return v
		}
	}
	if c.AuthStrategy == AuthEnv {
		if v := os.Getenv(envPassword); v != "" {
			return v
		}
	}
	return c.Password
}

func (c *Config) addrs() []string {
	if len(c.Hosts) > 0 {
		return c.Hosts
	}
	return []string{c.Host}
}

// String returns a log-safe representation without sensitive credentials.
func (c *Config) String() string {
	if c == nil {
		return "ClickHouse{nil}"
	}
	return fmt.Sprintf("clickhouse{db=%s table=%s auth=%s tls=%v batch_size=%d}",
		c.Database, c.Table, c.AuthStrategy, c.TLS, c.BatchSize)
}
