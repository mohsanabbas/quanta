package s3

import (
	"errors"
	"time"

	qerr "quanta/internal/errors"
)

// Format constants for S3 output.
const (
	FormatJSONL   = "jsonl"
	FormatParquet = "parquet"
)

type Config struct {
	Bucket     string `yaml:"bucket"`
	Region     string `yaml:"region"`
	Prefix     string `yaml:"prefix"`
	FileSuffix string `yaml:"file_suffix"`

	Format     string `yaml:"format"`
	SchemaFile string `yaml:"schema_file"` // Required for parquet format

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

const (
	_defaultBatchSize     = 100
	_defaultFlushInterval = 5 * time.Second
)

func (c *Config) validate() error {
	if c.Bucket == "" {
		return qerr.Config("s3", "validate", errors.New("bucket name is required"))
	}
	if c.Region == "" && c.Endpoint == "" {
		return qerr.Config("s3", "validate", errors.New("region or custom endpoint is required"))
	}
	if c.BatchSize <= 0 {
		c.BatchSize = _defaultBatchSize
	}
	if c.FlushInterval <= 0 {
		c.FlushInterval = _defaultFlushInterval
	}
	if c.Format == "" {
		c.Format = FormatJSONL
	}
	if c.Format == FormatParquet && c.SchemaFile == "" {
		return qerr.Config("s3", "validate", errors.New("schema_file is required for parquet format"))
	}

	switch c.AuthStrategy {
	case AuthStaticCreds:
		if c.AccessKeyID == "" || c.SecretAccessKey == "" {
			return qerr.Config("s3", "validate", errors.New("access_key_id and secret_access_key are required for static auth"))
		}
	case AuthIAMRole, AuthEnvVars:

	default:
		return qerr.Config("s3", "validate", errors.New("unsupported auth_strategy"))
	}

	return nil
}
