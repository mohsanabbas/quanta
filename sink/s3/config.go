package s3

import (
	"errors"
	"fmt"
	"time"

	qerr "quanta/internal/errors"
)

type AuthStrategy string

const (
	AuthIAMRole     AuthStrategy = "iam-role"
	AuthStaticCreds AuthStrategy = "static"
	AuthEnvVars     AuthStrategy = "env"
)

type CompressionType string

const (
	CompressionNone   CompressionType = "none"
	CompressionGzip   CompressionType = "gzip"
	CompressionSnappy CompressionType = "snappy"
)

type Config struct {
	Bucket     string `yaml:"bucket"`
	Region     string `yaml:"region"`
	Prefix     string `yaml:"prefix"`
	FileSuffix string `yaml:"file_suffix"`

	Format string `yaml:"format"`

	BatchSize     int           `yaml:"batch_size"`
	FlushInterval time.Duration `yaml:"flush_interval"`
	MaxFileAge    time.Duration `yaml:"max_file_age"`

	AuthStrategy    AuthStrategy `yaml:"auth_strategy"`
	AccessKeyID     string       `yaml:"access_key_id"`
	SecretAccessKey string       `yaml:"secret_access_key"`

	Endpoint      string          `yaml:"endpoint"`
	PathStyle     bool            `yaml:"path_style"`
	Compression   CompressionType `yaml:"compression"`
	EncryptionSSE string          `yaml:"encryption_sse"`
	KMSKeyID      string          `yaml:"kms_key_id"`
}

func (c *Config) validate() error {
	if c.Bucket == "" {
		return qerr.Config("s3", "validate", errors.New("bucket name is required"))
	}
	if c.Region == "" && c.Endpoint == "" {
		return qerr.Config("s3", "validate", errors.New("region or custom endpoint is required"))
	}
	if c.BatchSize <= 0 {
		return qerr.Config("s3", "validate", errors.New("batch_size must be greater than 0"))
	}
	if c.FlushInterval <= 0 {
		return qerr.Config("s3", "validate", errors.New("flush_interval must be greater than 0"))
	}
	if c.Format == "" {
		c.Format = "jsonl"
	}
	if _, err := newEncoder(c.Format); err != nil {
		return qerr.Config("s3", "validate", err)
	}

	switch c.AuthStrategy {
	case AuthStaticCreds:
		if c.AccessKeyID == "" || c.SecretAccessKey == "" {
			return qerr.Config("s3", "validate", errors.New("access_key_id and secret_access_key are required for static auth"))
		}
	case AuthIAMRole, AuthEnvVars:

	default:
		return qerr.Config("s3", "validate", fmt.Errorf("invalid auth_strategy: %s", c.AuthStrategy))
	}

	return nil
}
