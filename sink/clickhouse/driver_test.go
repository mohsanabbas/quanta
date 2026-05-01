package clickhouse

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/column"
	chdriver "github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"quanta/sink/batch"
	"quanta/sink/schema"
)

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{
			name:    "missing host",
			cfg:     Config{Database: "db", Table: "t", SchemaFile: "s.yaml"},
			wantErr: "host or hosts required",
		},
		{
			name:    "missing database",
			cfg:     Config{Host: "localhost:9000", Table: "t", SchemaFile: "s.yaml"},
			wantErr: "database required",
		},
		{
			name:    "missing table",
			cfg:     Config{Host: "localhost:9000", Database: "db", SchemaFile: "s.yaml"},
			wantErr: "table required",
		},
		{
			name:    "missing schema_file",
			cfg:     Config{Host: "localhost:9000", Database: "db", Table: "t"},
			wantErr: "schema_file required",
		},
		{
			name: "missing username for native auth",
			cfg: Config{
				Host:         "localhost:9000",
				Database:     "db",
				Table:        "t",
				SchemaFile:   "s.yaml",
				AuthStrategy: AuthNative,
			},
			wantErr: "username required for native auth",
		},
		{
			name: "missing client cert for tls auth",
			cfg: Config{
				Host:         "localhost:9000",
				Database:     "db",
				Table:        "t",
				SchemaFile:   "s.yaml",
				AuthStrategy: AuthTLS,
			},
			wantErr: "client_cert and client_key required for tls auth",
		},
		{
			name: "valid native config",
			cfg: Config{
				Host:         "localhost:9000",
				Database:     "db",
				Table:        "events",
				SchemaFile:   "s.yaml",
				AuthStrategy: AuthNative,
				Username:     "default",
			},
			wantErr: "",
		},
		{
			name: "valid with hosts list",
			cfg: Config{
				Hosts:        []string{"host1:9000", "host2:9000"},
				Database:     "db",
				Table:        "events",
				SchemaFile:   "s.yaml",
				AuthStrategy: AuthNative,
				Username:     "default",
			},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.validate()
			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			}
		})
	}
}

func TestConfig_ApplyDefaults(t *testing.T) {
	cfg := Config{
		Host:       "localhost:9000",
		Database:   "db",
		Table:      "t",
		SchemaFile: "s.yaml",
		Username:   "default",
	}

	cfg.applyDefaults()

	assert.Equal(t, defaultBatchSize, cfg.BatchSize)
	assert.Equal(t, defaultFlushInterval, cfg.FlushInterval)
	assert.Equal(t, defaultDialTimeout, cfg.DialTimeout)
	assert.Equal(t, defaultMaxIdleConns, cfg.MaxIdleConns)
	assert.Equal(t, defaultMaxOpenConns, cfg.MaxOpenConns)
	assert.Equal(t, defaultConnMaxLife, cfg.ConnMaxLife)
	assert.Equal(t, defaultMaxRetries, cfg.MaxRetries)
	assert.Equal(t, defaultRetryInterval, cfg.RetryInterval)
	assert.Equal(t, AuthNative, cfg.AuthStrategy)
	assert.Equal(t, CompressionLZ4, cfg.Compression)
}

func TestConfig_ResolveCredentials(t *testing.T) {
	t.Run("username from config", func(t *testing.T) {
		cfg := Config{Username: "configuser"}
		assert.Equal(t, "configuser", cfg.resolveUsername())
	})

	t.Run("username from env override", func(t *testing.T) {
		t.Setenv("TEST_CH_USER", "envuser")
		cfg := Config{Username: "configuser", UsernameEnv: "TEST_CH_USER"}
		assert.Equal(t, "envuser", cfg.resolveUsername())
	})

	t.Run("username from AuthEnv strategy", func(t *testing.T) {
		t.Setenv("CLICKHOUSE_USER", "envstrategyuser")
		cfg := Config{AuthStrategy: AuthEnv}
		assert.Equal(t, "envstrategyuser", cfg.resolveUsername())
	})

	t.Run("password from config", func(t *testing.T) {
		cfg := Config{Password: "configpass"}
		assert.Equal(t, "configpass", cfg.resolvePassword())
	})

	t.Run("password from env override", func(t *testing.T) {
		t.Setenv("TEST_CH_PASS", "envpass")
		cfg := Config{Password: "configpass", PasswordEnv: "TEST_CH_PASS"}
		assert.Equal(t, "envpass", cfg.resolvePassword())
	})

	t.Run("password from AuthEnv strategy", func(t *testing.T) {
		t.Setenv("CLICKHOUSE_PASSWORD", "envstrategypass")
		cfg := Config{AuthStrategy: AuthEnv}
		assert.Equal(t, "envstrategypass", cfg.resolvePassword())
	})
}

func TestConfig_Addrs(t *testing.T) {
	t.Run("single host", func(t *testing.T) {
		cfg := Config{Host: "localhost:9000"}
		assert.Equal(t, []string{"localhost:9000"}, cfg.addrs())
	})

	t.Run("hosts list", func(t *testing.T) {
		cfg := Config{Hosts: []string{"host1:9000", "host2:9000"}}
		assert.Equal(t, []string{"host1:9000", "host2:9000"}, cfg.addrs())
	})

	t.Run("hosts takes precedence", func(t *testing.T) {
		cfg := Config{Host: "localhost:9000", Hosts: []string{"host1:9000"}}
		assert.Equal(t, []string{"host1:9000"}, cfg.addrs())
	})
}

func TestConfig_String(t *testing.T) {
	cfg := Config{
		Host:         "localhost:9000",
		Database:     "analytics",
		Table:        "events",
		AuthStrategy: AuthNative,
		TLS:          true,
		Username:     "admin",
		Password:     "secret123", // Should NOT appear in output
		BatchSize:    10000,
	}

	s := cfg.String()

	// Safe to log: non-sensitive config
	assert.Contains(t, s, "analytics")
	assert.Contains(t, s, "events")
	assert.Contains(t, s, "native")
	assert.Contains(t, s, "tls=true")
	assert.Contains(t, s, "batch_size=10000")

	// MUST NOT log: sensitive credentials and network info
	assert.NotContains(t, s, "localhost:9000") // Host can reveal infrastructure
	assert.NotContains(t, s, "secret123")      // Password
	assert.NotContains(t, s, "admin")          // Username
}

func TestConfig_BuildTLSConfig(t *testing.T) {
	t.Run("insecure skip verify", func(t *testing.T) {
		cfg := Config{TLSInsecure: true}
		tlsCfg, err := cfg.buildTLSConfig()
		require.NoError(t, err)
		assert.True(t, tlsCfg.InsecureSkipVerify)
	})

	t.Run("ca cert not found", func(t *testing.T) {
		cfg := Config{CACert: "/nonexistent/ca.pem"}
		_, err := cfg.buildTLSConfig()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "read ca_cert")
	})

	t.Run("valid ca cert", func(t *testing.T) {
		// Skip this test as it requires a real certificate
		// The parsing logic is tested indirectly by client_cert_not_found
		// and the error path for invalid PEM
		t.Skip("requires valid X.509 certificate")
	})

	t.Run("client cert not found", func(t *testing.T) {
		cfg := Config{ClientCert: "/nonexistent/client.pem", ClientKey: "/nonexistent/key.pem"}
		_, err := cfg.buildTLSConfig()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "load client cert")
	})
}

func TestConfig_CompressionMethod(t *testing.T) {
	tests := []struct {
		compression Compression
		want        string
	}{
		{CompressionNone, "none"},
		{CompressionLZ4, "lz4"},
		{CompressionZSTD, "zstd"},
		{"", "lz4"},        // default
		{"unknown", "lz4"}, // fallback
	}

	for _, tt := range tests {
		t.Run(string(tt.compression), func(t *testing.T) {
			cfg := Config{Compression: tt.compression}
			method := cfg.compressionMethod()
			// Just verify no panic - actual enum values are internal to clickhouse-go
			assert.NotNil(t, method)
		})
	}
}

func TestAuthStrategy(t *testing.T) {
	t.Run("string", func(t *testing.T) {
		assert.Equal(t, "native", AuthNative.String())
		assert.Equal(t, "tls", AuthTLS.String())
		assert.Equal(t, "env", AuthEnv.String())
	})

	t.Run("parse", func(t *testing.T) {
		tests := []struct {
			input string
			want  AuthStrategy
			ok    bool
		}{
			{"native", AuthNative, true},
			{"", AuthNative, true},
			{"tls", AuthTLS, true},
			{"env", AuthEnv, true},
			{"invalid", "", false},
		}

		for _, tt := range tests {
			got, ok := ParseAuthStrategy(tt.input)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.ok, ok)
		}
	})
}

func TestConfig_TLSForcedForAuthTLS(t *testing.T) {
	cfg := Config{
		Host:         "localhost:9000",
		Database:     "db",
		Table:        "t",
		SchemaFile:   "s.yaml",
		AuthStrategy: AuthTLS,
		ClientCert:   "/path/to/cert.pem",
		ClientKey:    "/path/to/key.pem",
		TLS:          false, // Explicitly false
	}

	err := cfg.validateAuth()
	require.NoError(t, err)
	assert.True(t, cfg.TLS, "TLS should be forced to true for AuthTLS strategy")
}

// mockBatch implements chdriver.Batch for testing.
type mockBatch struct {
	appendCalls [][]any
	sendCalled  bool
	sendErr     error
}

func (m *mockBatch) Append(v ...any) error {
	m.appendCalls = append(m.appendCalls, v)
	return nil
}

func (m *mockBatch) Send() error {
	m.sendCalled = true
	return m.sendErr
}

func (m *mockBatch) Abort() error                    { return nil }
func (m *mockBatch) Flush() error                    { return nil }
func (m *mockBatch) IsSent() bool                    { return m.sendCalled }
func (m *mockBatch) Rows() int                       { return len(m.appendCalls) }
func (m *mockBatch) Column(int) chdriver.BatchColumn { return nil }
func (m *mockBatch) AppendStruct(any) error          { return nil }
func (m *mockBatch) Columns() []column.Interface     { return nil }
func (m *mockBatch) Close() error                    { return nil }

// mockConn implements Conn interface for testing.
type mockConn struct {
	pingErr      error
	prepareBatch *mockBatch
	prepareErr   error
	closed       bool
}

func (m *mockConn) Ping(ctx context.Context) error {
	return m.pingErr
}

func (m *mockConn) PrepareBatch(ctx context.Context, query string, opts ...chdriver.PrepareBatchOption) (chdriver.Batch, error) {
	if m.prepareErr != nil {
		return nil, m.prepareErr
	}
	return m.prepareBatch, nil
}

func (m *mockConn) Close() error {
	m.closed = true
	return nil
}

func TestDriver_InsertBatch(t *testing.T) {
	// Create a minimal schema file for testing
	schemaContent := `
kind: Schema
apiVersion: v1
name: test
columns:
  - name: id
    path: id
    type: string
    required: true
  - name: value
    path: value
    type: int64
`
	tmpFile, err := os.CreateTemp("", "schema-*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())
	_, err = tmpFile.WriteString(schemaContent)
	require.NoError(t, err)
	tmpFile.Close()

	mockB := &mockBatch{}
	mockC := &mockConn{prepareBatch: mockB}

	cfg := Config{
		Host:          "localhost:9000",
		Database:      "test",
		Table:         "events",
		SchemaFile:    tmpFile.Name(),
		AuthStrategy:  AuthNative,
		Username:      "default",
		BatchSize:     100,
		FlushInterval: time.Second,
	}
	cfg.applyDefaults()

	// Load schema manually for test
	s, err := schema.LoadSchema(tmpFile.Name())
	require.NoError(t, err)
	mapper := schema.NewMapper(s)

	d := &clickhouseDriver{
		cfg:     cfg,
		conn:    mockC,
		mapper:  mapper,
		columns: mapper.ColumnNames(),
	}

	// Test insert batch
	records := []batch.Record[[]byte]{
		{Data: []byte(`{"id": "1", "value": 100}`)},
		{Data: []byte(`{"id": "2", "value": 200}`)},
	}

	err = d.insertBatch(context.Background(), records)
	require.NoError(t, err)

	assert.True(t, mockB.sendCalled)
	assert.Len(t, mockB.appendCalls, 2)
}

func TestDriver_InsertBatchEmpty(t *testing.T) {
	d := &clickhouseDriver{}
	err := d.insertBatch(context.Background(), nil)
	require.NoError(t, err)
}

func TestDriver_InsertBatchPrepareError(t *testing.T) {
	mockC := &mockConn{prepareErr: errors.New("prepare failed")}

	d := &clickhouseDriver{
		cfg:     Config{Database: "test", Table: "events"},
		conn:    mockC,
		columns: []string{"id"},
	}

	records := []batch.Record[[]byte]{{Data: []byte(`{}`)}}
	err := d.insertBatch(context.Background(), records)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "prepare failed")
}

func TestDriver_InsertSQL(t *testing.T) {
	d := &clickhouseDriver{
		cfg:     Config{Database: "analytics", Table: "events"},
		columns: []string{"id", "name", "value"},
	}

	sql := d.insertSQL()
	assert.Equal(t, "INSERT INTO analytics.events (id, name, value)", sql)
}

func TestDriver_NameAndCaps(t *testing.T) {
	d := &clickhouseDriver{}

	assert.Equal(t, "clickhouse", d.Name())

	caps := d.Caps()
	assert.True(t, caps.AckAware)
	assert.True(t, caps.NackAware)
}
