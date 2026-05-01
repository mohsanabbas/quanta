// Package s3 provides an AWS S3 sink driver for Quanta.
//
// Supports JSONL and Parquet formats. Parquet requires a schema file
// aligned with Open Data Contract Standard (ODCS).
package s3

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	s3svc "github.com/aws/aws-sdk-go-v2/service/s3"

	pb "quanta/api/proto/v1"
	qerr "quanta/internal/errors"
	"quanta/internal/logging"
	"quanta/sink"
	"quanta/sink/batch"
	"quanta/sink/schema"
)

// Client is the narrow S3 surface the driver needs.
type Client interface {
	PutObject(ctx context.Context, params *s3svc.PutObjectInput, optFns ...func(*s3svc.Options)) (*s3svc.PutObjectOutput, error)
}

type s3Driver struct {
	cfg     Config
	client  Client
	encoder Encoder
	flusher *batch.Flusher[[]byte]
}

var _ sink.Adapter = (*s3Driver)(nil)

func (d *s3Driver) Name() string { return "s3" }

func (d *s3Driver) Caps() sink.Capabilities {
	return sink.Capabilities{AckAware: true, NackAware: true}
}

func newDriver(ctx context.Context, cfg Config, opts sink.BuildOptions) (sink.Adapter, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	client, err := newS3Client(ctx, &cfg)
	if err != nil {
		return nil, qerr.Sink("s3", "connect", err)
	}

	// Load schema for parquet format
	var mapper *schema.Mapper
	if cfg.Format == "parquet" {
		s, err := schema.LoadSchema(cfg.SchemaFile)
		if err != nil {
			return nil, qerr.Sink("s3", "load_schema", err)
		}
		mapper = schema.NewMapper(s)
	}

	enc, err := newEncoder(cfg.Format, mapper)
	if err != nil {
		return nil, qerr.Sink("s3", "configure", err)
	}

	return newDriverWithClient(ctx, cfg, client, enc, opts), nil
}

func newDriverWithClient(ctx context.Context, cfg Config, client Client, enc Encoder, opts sink.BuildOptions) *s3Driver {
	d := &s3Driver{
		cfg:     cfg,
		client:  client,
		encoder: enc,
	}

	flushFn := func(fctx context.Context, records []batch.Record[[]byte]) error {
		return d.upload(fctx, records)
	}

	d.flusher = batch.NewFlusher(batch.FlusherConfig{
		BatchSize:     cfg.BatchSize,
		FlushInterval: cfg.FlushInterval,
	}, flushFn, batch.Callbacks{Ack: opts.Ack, Nack: opts.Nack})

	d.flusher.Start(ctx)
	return d
}

func (d *s3Driver) Publish(ctx context.Context, f *pb.Frame) error {
	return d.flusher.Add(ctx, bytes.Clone(f.Value), f.Checkpoint, f, int64(len(f.Value)))
}

func (d *s3Driver) Close(ctx context.Context) error {
	return d.flusher.Close(ctx)
}

func (d *s3Driver) upload(ctx context.Context, records []batch.Record[[]byte]) error {
	if len(records) == 0 {
		return nil
	}

	data := make([][]byte, len(records))
	for i, r := range records {
		data[i] = r.Data
	}

	encoded, err := d.encoder.Encode(data)
	if err != nil {
		logging.L().ErrorContext(ctx, "s3 sink: encode failed",
			"error", err, "frames", len(records))
		return err
	}

	key := d.objectKey()

	_, err = d.client.PutObject(ctx, &s3svc.PutObjectInput{
		Bucket:      aws.String(d.cfg.Bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(encoded),
		ContentType: aws.String(d.encoder.ContentType()),
	})
	if err != nil {
		logging.L().ErrorContext(ctx, "s3 sink: upload failed",
			"error", err, "key", key, "frames", len(records))
		return err
	}

	return nil
}

func (d *s3Driver) objectKey() string {
	suffix := d.cfg.FileSuffix
	if suffix == "" {
		switch d.cfg.Format {
		case "parquet":
			suffix = ".parquet"
		default:
			suffix = ".jsonl"
		}
	}
	name := fmt.Sprintf("%d_data%s", time.Now().UnixNano(), suffix)
	prefix := strings.TrimRight(d.cfg.Prefix, "/")
	if prefix == "" {
		return name
	}
	return prefix + "/" + name
}

func newS3Client(ctx context.Context, cfg *Config) (*s3svc.Client, error) {
	var opts []func(*awscfg.LoadOptions) error

	opts = append(opts, awscfg.WithRegion(cfg.Region))

	if cfg.Endpoint != "" {
		opts = append(opts, awscfg.WithBaseEndpoint(cfg.Endpoint))
	}

	switch cfg.AuthStrategy {
	case AuthStaticCreds:
		opts = append(opts, awscfg.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		))
	case AuthIAMRole, AuthEnvVars:
		// use default credentials
	}

	ac, err := awscfg.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, err
	}

	var s3opts []func(*s3svc.Options)
	if cfg.PathStyle {
		s3opts = append(s3opts, func(o *s3svc.Options) {
			o.UsePathStyle = true
		})
	}

	return s3svc.NewFromConfig(ac, s3opts...), nil
}
