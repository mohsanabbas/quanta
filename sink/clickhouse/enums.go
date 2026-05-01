package clickhouse

// AuthStrategy defines authentication methods for ClickHouse.
type AuthStrategy string

const (
	// AuthNative uses username/password authentication.
	AuthNative AuthStrategy = "native"
	// AuthTLS uses mTLS client certificates.
	AuthTLS AuthStrategy = "tls"
	// AuthEnv reads credentials from environment variables.
	AuthEnv AuthStrategy = "env"
)

func (a AuthStrategy) String() string { return string(a) }

// ParseAuthStrategy converts a string to AuthStrategy.
func ParseAuthStrategy(s string) (AuthStrategy, bool) {
	switch s {
	case "native", "":
		return AuthNative, true
	case "tls":
		return AuthTLS, true
	case "env":
		return AuthEnv, true
	default:
		return "", false
	}
}

// Compression defines compression methods for ClickHouse connections.
type Compression string

const (
	CompressionNone Compression = "none"
	CompressionLZ4  Compression = "lz4"
	CompressionZSTD Compression = "zstd"
)

func (c Compression) String() string { return string(c) }
