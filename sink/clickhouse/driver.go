package clickhouse

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/ClickHouse/clickhouse-go/v2"
	chdriver "github.com/ClickHouse/clickhouse-go/v2/lib/driver"

	pb "quanta/api/proto/v1"
	"quanta/internal/errors"
	"quanta/internal/logging"
	"quanta/sink"
	"quanta/sink/batch"
	"quanta/sink/schema"
)

// Conn abstracts the ClickHouse connection for testing.
type Conn interface {
	Ping(ctx context.Context) error
	PrepareBatch(ctx context.Context, query string, opts ...chdriver.PrepareBatchOption) (chdriver.Batch, error)
	Close() error
}

type clickhouseDriver struct {
	cfg     Config
	conn    Conn
	mapper  *schema.Mapper
	flusher *batch.Flusher[[]byte]
	columns []string
}

// Compile-time interface check.
var _ sink.Adapter = (*clickhouseDriver)(nil)

func newDriver(ctx context.Context, cfg Config, opts sink.BuildOptions) (*clickhouseDriver, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	// Load schema
	s, err := schema.LoadSchema(cfg.SchemaFile)
	if err != nil {
		return nil, errors.Sink("clickhouse", "load_schema", err)
	}
	mapper := schema.NewMapper(s)

	// Build connection options
	chOpts, err := cfg.buildOptions()
	if err != nil {
		return nil, errors.Sink("clickhouse", "build_options", err)
	}

	// Connect
	conn, err := clickhouse.Open(chOpts)
	if err != nil {
		return nil, errors.Sink("clickhouse", "connect", err)
	}

	// Ping to verify connection
	if err := conn.Ping(ctx); err != nil {
		if closeErr := conn.Close(); closeErr != nil {
			logging.L().WarnContext(ctx, "clickhouse: close after ping failure",
				"close_error", closeErr)
		}
		return nil, errors.Sink("clickhouse", "ping", err)
	}

	logging.L().InfoContext(ctx, "clickhouse sink connected",
		"database", cfg.Database,
		"table", cfg.Table,
		"batch_size", cfg.BatchSize,
		"compression", cfg.Compression)

	return newDriverWithConn(ctx, cfg, conn, mapper, opts), nil
}

func newDriverWithConn(ctx context.Context, cfg Config, conn Conn, mapper *schema.Mapper, opts sink.BuildOptions) *clickhouseDriver {
	d := &clickhouseDriver{
		cfg:     cfg,
		conn:    conn,
		mapper:  mapper,
		columns: mapper.ColumnNames(),
	}

	flushFn := func(fctx context.Context, records []batch.Record[[]byte]) error {
		return d.insertBatch(fctx, records)
	}

	d.flusher = batch.NewFlusher(batch.FlusherConfig{
		BatchSize:     cfg.BatchSize,
		FlushInterval: cfg.FlushInterval,
	}, flushFn, batch.Callbacks{Ack: opts.Ack, Nack: opts.Nack})

	d.flusher.Start(ctx)
	return d
}

func (c *Config) buildOptions() (*clickhouse.Options, error) {
	opts := &clickhouse.Options{
		Addr: c.addrs(),
		Auth: clickhouse.Auth{
			Database: c.Database,
			Username: c.resolveUsername(),
			Password: c.resolvePassword(),
		},
		DialTimeout:     c.DialTimeout,
		MaxIdleConns:    c.MaxIdleConns,
		MaxOpenConns:    c.MaxOpenConns,
		ConnMaxLifetime: c.ConnMaxLife,
		Compression: &clickhouse.Compression{
			Method: c.compressionMethod(),
		},
	}

	if c.TLS || c.AuthStrategy == AuthTLS {
		tlsConfig, err := c.buildTLSConfig()
		if err != nil {
			return nil, err
		}
		opts.TLS = tlsConfig
	}

	return opts, nil
}

func (c *Config) compressionMethod() clickhouse.CompressionMethod {
	switch c.Compression {
	case CompressionLZ4:
		return clickhouse.CompressionLZ4
	case CompressionZSTD:
		return clickhouse.CompressionZSTD
	case CompressionNone:
		return clickhouse.CompressionNone
	default:
		return clickhouse.CompressionLZ4
	}
}

func (d *clickhouseDriver) Name() string { return "clickhouse" }

func (d *clickhouseDriver) Caps() sink.Capabilities {
	return sink.Capabilities{
		AckAware:  true,
		NackAware: true,
	}
}

func (d *clickhouseDriver) Publish(ctx context.Context, f *pb.Frame) error {
	return d.flusher.Add(ctx, bytes.Clone(f.Value), f.Checkpoint, f, int64(len(f.Value)))
}

func (d *clickhouseDriver) Close(ctx context.Context) error {
	if err := d.flusher.Close(ctx); err != nil {
		return err
	}
	return d.conn.Close()
}

func (d *clickhouseDriver) insertBatch(ctx context.Context, records []batch.Record[[]byte]) error {
	if len(records) == 0 {
		return nil
	}

	// Prepare batch
	batchStmt, err := d.conn.PrepareBatch(ctx, d.insertSQL())
	if err != nil {
		logging.L().ErrorContext(ctx, "clickhouse: prepare batch failed",
			"error", err, "records", len(records))
		return err
	}

	// Append rows
	for _, rec := range records {
		vals, err := d.mapper.ExtractValues(rec.Data)
		if err != nil {
			logging.L().ErrorContext(ctx, "clickhouse: extract values failed",
				"error", err)
			return err
		}
		if err := batchStmt.Append(vals...); err != nil {
			logging.L().ErrorContext(ctx, "clickhouse: append row failed",
				"error", err)
			return err
		}
	}

	// Send batch
	if err := batchStmt.Send(); err != nil {
		logging.L().ErrorContext(ctx, "clickhouse: send batch failed",
			"error", err, "records", len(records))
		return err
	}

	logging.L().DebugContext(ctx, "clickhouse: batch inserted",
		"records", len(records), "table", d.cfg.Table)

	return nil
}

func (d *clickhouseDriver) insertSQL() string {
	cols := strings.Join(d.columns, ", ")
	return fmt.Sprintf("INSERT INTO %s.%s (%s)", quoteIdent(d.cfg.Database), quoteIdent(d.cfg.Table), cols)
}

// quoteIdent quotes a ClickHouse identifier with backticks.
func quoteIdent(s string) string {
	return "`" + strings.ReplaceAll(s, "`", "``") + "`"
}
