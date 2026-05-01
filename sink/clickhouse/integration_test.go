//go:build integration

package clickhouse

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	pb "quanta/api/proto/v1"
	"quanta/sink"
	"quanta/sink/batch"
)

// Integration tests require a running ClickHouse instance.
// Run with: go test -tags=integration ./sink/clickhouse/...
//
// Docker command to start ClickHouse:
//   docker run -d --name clickhouse-test \
//     -p 9000:9000 -p 8123:8123 \
//     -e CLICKHOUSE_USER=default \
//     -e CLICKHOUSE_PASSWORD=test123 \
//     clickhouse/clickhouse-server:24.3
//
// Then create test table:
//   docker exec -i clickhouse-test clickhouse-client --password test123 <<EOF
//   CREATE DATABASE IF NOT EXISTS test_db;
//   CREATE TABLE IF NOT EXISTS test_db.events (
//       id String,
//       value Int64,
//       created_at DateTime64(6, 'UTC')
//   ) ENGINE = MergeTree()
//   ORDER BY id;
//   EOF

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func skipIfNoClickHouse(t *testing.T) {
	host := os.Getenv("CLICKHOUSE_HOST")
	if host == "" {
		host = "localhost:9000"
	}

	// Try to connect with short timeout to detect availability
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{host},
		Auth: clickhouse.Auth{
			Username: func() string {
				if u := os.Getenv("CLICKHOUSE_USER"); u != "" {
					return u
				}
				return "default"
			}(),
			Password: func() string {
				if p := os.Getenv("CLICKHOUSE_PASSWORD"); p != "" {
					return p
				}
				return "test123"
			}(),
		},
	})
	if err != nil {
		t.Skipf("ClickHouse not available: %v", err)
	}
	defer conn.Close()

	if err := conn.Ping(ctx); err != nil {
		t.Skipf("ClickHouse not reachable: %v", err)
	}

	t.Setenv("CLICKHOUSE_TEST_HOST", host)
	t.Setenv("CLICKHOUSE_TEST_USER", func() string {
		if u := os.Getenv("CLICKHOUSE_USER"); u != "" {
			return u
		}
		return "default"
	}())
	t.Setenv("CLICKHOUSE_TEST_PASSWORD", func() string {
		if p := os.Getenv("CLICKHOUSE_PASSWORD"); p != "" {
			return p
		}
		return "test123"
	}())
}

func TestIntegration_Connect(t *testing.T) {
	skipIfNoClickHouse(t)

	// Create test schema file
	schemaContent := `
kind: Schema
apiVersion: v1
name: test_events
columns:
  - name: id
    path: id
    type: string
    required: true
  - name: value
    path: value
    type: int64
  - name: created_at
    path: created_at
    type: timestamp
`
	tmpFile, err := os.CreateTemp("", "schema-*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())
	_, err = tmpFile.WriteString(schemaContent)
	require.NoError(t, err)
	tmpFile.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cfg := Config{
		Host:          os.Getenv("CLICKHOUSE_TEST_HOST"),
		Database:      "test_db",
		Table:         "events",
		Username:      os.Getenv("CLICKHOUSE_TEST_USER"),
		Password:      os.Getenv("CLICKHOUSE_TEST_PASSWORD"),
		AuthStrategy:  AuthNative,
		SchemaFile:    tmpFile.Name(),
		BatchSize:     100,
		FlushInterval: time.Second,
	}

	nackCh := make(chan error, 1)
	opts := sink.BuildOptions{
		Ack: func(_ context.Context, _ *pb.CheckpointToken) {
			// Ack received
		},
		Nack: func(_ context.Context, _ *pb.Frame, err error) {
			select {
			case nackCh <- err:
			default:
			}
		},
	}

	driver, err := newDriver(ctx, cfg, opts)
	require.NoError(t, err)

	assert.Equal(t, "clickhouse", driver.Name())
	assert.True(t, driver.Caps().AckAware)

	require.NoError(t, driver.Close(ctx))

	select {
	case err := <-nackCh:
		require.NoError(t, err, "unexpected nack")
	default:
	}
}

func TestIntegration_InsertBatch(t *testing.T) {
	skipIfNoClickHouse(t)

	// Create test schema file
	schemaContent := `
kind: Schema
apiVersion: v1
name: test_events
columns:
  - name: id
    path: id
    type: string
    required: true
  - name: value
    path: value
    type: int64
  - name: created_at
    path: created_at
    type: timestamp
`
	tmpFile, err := os.CreateTemp("", "schema-*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())
	_, err = tmpFile.WriteString(schemaContent)
	require.NoError(t, err)
	tmpFile.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cfg := Config{
		Host:          os.Getenv("CLICKHOUSE_TEST_HOST"),
		Database:      "test_db",
		Table:         "events",
		Username:      os.Getenv("CLICKHOUSE_TEST_USER"),
		Password:      os.Getenv("CLICKHOUSE_TEST_PASSWORD"),
		AuthStrategy:  AuthNative,
		SchemaFile:    tmpFile.Name(),
		BatchSize:     10, // Small batch for testing
		FlushInterval: 100 * time.Millisecond,
	}

	ackCount := 0
	opts := sink.BuildOptions{
		Ack: func(ctx context.Context, tok *pb.CheckpointToken) {
			ackCount++
		},
		Nack: func(ctx context.Context, frame *pb.Frame, err error) {
			t.Errorf("unexpected nack: %v", err)
		},
	}

	driver, err := newDriver(ctx, cfg, opts)
	require.NoError(t, err)

	// Insert test records
	records := []batch.Record[[]byte]{
		{Data: []byte(`{"id": "int-test-1", "value": 100, "created_at": "2024-01-01T00:00:00Z"}`)},
		{Data: []byte(`{"id": "int-test-2", "value": 200, "created_at": "2024-01-01T00:00:01Z"}`)},
		{Data: []byte(`{"id": "int-test-3", "value": 300, "created_at": "2024-01-01T00:00:02Z"}`)},
	}

	err = driver.insertBatch(ctx, records)
	require.NoError(t, err)

	// Close to flush any remaining
	err = driver.Close(ctx)
	require.NoError(t, err)

	t.Log("Integration test passed - records inserted to ClickHouse")
}
