package s3

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	s3svc "github.com/aws/aws-sdk-go-v2/service/s3"

	pb "quanta/api/proto/v1"
	qerr "quanta/internal/errors"
	"quanta/internal/logging"
	"quanta/sink"
)

type Client interface {
	PutObject(ctx context.Context, params *s3svc.PutObjectInput, optFns ...func(*s3svc.Options)) (*s3svc.PutObjectOutput, error)
}

var (
	_ sink.Adapter  = (*Driver)(nil)
	_ sink.AckAware = (*Driver)(nil)
)

type Driver struct {
	cfg     Config
	client  Client
	encoder Encoder
	ack     sink.EmitFn
	pool    *sync.Pool

	mu      sync.Mutex
	current *batch
	sealCh  chan *batch
	stopCh  chan struct{}
	doneCh  chan struct{}
}

func (d *Driver) Configure(ctx context.Context, raw any) error {
	var cfg Config
	switch v := raw.(type) {
	case Config:
		cfg = v
	case *Config:
		if v != nil {
			cfg = *v
		}
	default:
		got := reflect.TypeOf(raw).String()
		logging.L().WarnContext(ctx, "invalid config type", "component", "sink.s3", "got", got)
		return qerr.Sink("s3", "configure", errors.New("invalid config type"))
	}

	if err := cfg.validate(); err != nil {
		return err
	}

	client, err := newS3Client(ctx, &cfg)
	if err != nil {
		return qerr.Sink("s3", "connect", err)
	}

	enc, err := newEncoder(cfg.Format)
	if err != nil {
		return qerr.Sink("s3", "configure", err)
	}

	pool := newBatchPool(cfg.BatchSize)

	d.cfg = cfg
	d.client = client
	d.encoder = enc
	d.pool = pool
	d.current = pool.Get().(*batch)
	d.sealCh = make(chan *batch, cfg.BatchSize)
	d.stopCh = make(chan struct{})
	d.doneCh = make(chan struct{})

	go d.flushLoop(context.WithoutCancel(ctx))

	return nil
}

func (d *Driver) BindAck(fn sink.EmitFn) {
	d.ack = fn
}

func (d *Driver) Publish(_ context.Context, f *pb.Frame) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.current.append(f.Value, f.Checkpoint)

	if d.current.full() {
		sealed := d.current
		d.current = d.pool.Get().(*batch)
		d.sealCh <- sealed
	}
	return nil
}

func (d *Driver) Close(ctx context.Context) error {
	close(d.stopCh)
	select {
	case <-d.doneCh:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
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

func (d *Driver) flushLoop(ctx context.Context) {
	defer close(d.doneCh)

	ticker := time.NewTicker(d.cfg.FlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-d.stopCh:
			d.drainSealed(ctx)
			d.flushPartial(ctx)
			return
		case sealed := <-d.sealCh:
			d.uploadBatch(ctx, sealed)
		case <-ticker.C:
			d.drainSealed(ctx)
			d.flushPartial(ctx)
		}
	}
}

func (d *Driver) drainSealed(ctx context.Context) {
	for {
		select {
		case sealed := <-d.sealCh:
			d.uploadBatch(ctx, sealed)
		default:
			return
		}
	}
}

func (d *Driver) flushPartial(ctx context.Context) {
	d.mu.Lock()
	if d.current.len() == 0 {
		d.mu.Unlock()
		return
	}
	partial := d.current
	d.current = d.pool.Get().(*batch)
	d.mu.Unlock()

	d.uploadBatch(ctx, partial)
}

func (d *Driver) uploadBatch(ctx context.Context, b *batch) {
	records := b.records[:b.len()]
	checkpoints := b.checkpoints[:b.len()]

	body, err := d.encoder.Encode(records)
	if err != nil {
		logging.L().WarnContext(ctx, "s3 sink: encode", "error", err)
		d.recycleBatch(b)
		return
	}

	data, err := io.ReadAll(body)
	if err != nil {
		logging.L().WarnContext(ctx, "s3 sink: read body", "error", err)
		d.recycleBatch(b)
		return
	}

	key := fmt.Sprintf("%s/%d_%s%s", d.cfg.Prefix, time.Now().UnixNano(), "data", d.cfg.FileSuffix)

	_, err = d.client.PutObject(ctx, &s3svc.PutObjectInput{
		Bucket:      aws.String(d.cfg.Bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(data),
		ContentType: aws.String(d.encoder.ContentType()),
	})
	if err != nil {
		logging.L().WarnContext(ctx, "s3 sink: upload", "error", err, "key", key)
		d.recycleBatch(b)
		return
	}

	if d.ack != nil {
		for _, tok := range checkpoints {
			d.ack(tok)
		}
	}

	d.recycleBatch(b)
}

func (d *Driver) recycleBatch(b *batch) {
	b.reset()
	d.pool.Put(b)
}
