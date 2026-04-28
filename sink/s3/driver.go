// Package s3 — AWS S3 sink driver.
//
// Constructed via sink.Build("s3", cfg, opts). The factory builds the AWS
// client, allocates the batch pool, starts the flushLoop goroutine, and
// returns a ready-to-Publish adapter. Any failure releases acquired resources
// before returning.
package s3

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
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

// Client is the narrow S3 surface the driver needs. Tests inject a spy.
type Client interface {
	PutObject(ctx context.Context, params *s3svc.PutObjectInput, optFns ...func(*s3svc.Options)) (*s3svc.PutObjectOutput, error)
}

type s3Driver struct {
	cfg     Config
	client  Client
	encoder Encoder
	ack     sink.EmitFn
	nack    sink.NackFn
	pool    *sync.Pool

	mu           sync.Mutex
	current      *batch
	closing      bool           // set under mu before close(stopCh) so Publish rejects new frames
	sealInflight sync.WaitGroup // counts sealed batches in-flight to sealCh; held across the send
	sealCh       chan *batch
	stopCh       chan struct{}
	stopOnce     sync.Once
	doneCh       chan struct{}
}

var _ sink.Adapter = (*s3Driver)(nil)

func (d *s3Driver) Name() string {
	return "s3"
}

func (d *s3Driver) Caps() sink.Capabilities {
	return sink.Capabilities{
		AckAware:  true,
		NackAware: true,
	}
}

func newDriver(ctx context.Context, cfg Config, opts sink.BuildOptions) (sink.Adapter, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	client, err := newS3Client(ctx, &cfg)
	if err != nil {
		return nil, qerr.Sink("s3", "connect", err)
	}
	enc, err := newEncoder(cfg.Format)
	if err != nil {
		return nil, qerr.Sink("s3", "configure", err)
	}
	return newDriverWithClient(ctx, cfg, client, enc, opts), nil
}

func newDriverWithClient(ctx context.Context, cfg Config, client Client, enc Encoder, opts sink.BuildOptions) *s3Driver {
	pool := newBatchPool(cfg.BatchSize)
	flushCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))

	d := &s3Driver{
		cfg:     cfg,
		client:  client,
		encoder: enc,
		ack:     opts.Ack,
		nack:    opts.Nack,
		pool:    pool,
		current: pool.Get().(*batch),
		sealCh:  make(chan *batch, 1),
		stopCh:  make(chan struct{}),
		doneCh:  make(chan struct{}),
	}
	// flushLoop owns flushCtx; cancel runs when the goroutine exits so the
	// detached context never outlives the goroutine that uses it.
	go func() {
		defer cancel()
		d.flushLoop(flushCtx)
	}()
	return d
}

func (d *s3Driver) Publish(ctx context.Context, f *pb.Frame) error {
	d.mu.Lock()
	if d.closing {
		d.mu.Unlock()
		return errors.New("s3 sink: closed")
	}
	d.current.append(f.Value, f.Checkpoint, f)
	var sealed *batch
	if d.current.full() {
		sealed = d.current
		d.current = d.pool.Get().(*batch)
		// Increment under d.mu so Close cannot observe sealInflight==0
		// after setting closing=true while a sealed batch is still in flight.
		d.sealInflight.Add(1)
	}
	d.mu.Unlock()

	if sealed == nil {
		return nil
	}
	defer d.sealInflight.Done()
	// Send outside d.mu so a full sealCh (cap 1) cannot block while holding
	// the lock. Close waits on sealInflight before closing stopCh, so the
	// flushLoop is guaranteed to be reading sealCh until this send completes
	// (or the caller's ctx cancels).
	select {
	case d.sealCh <- sealed:
		return nil
	case <-ctx.Done():
		d.recycleBatch(sealed)
		return ctx.Err()
	}
}

func (d *s3Driver) Close(ctx context.Context) error {
	d.mu.Lock()
	if d.closing {
		d.mu.Unlock()
	} else {
		d.closing = true
		d.mu.Unlock()
	}

	waitDone := make(chan struct{})
	go func() {
		defer close(waitDone)
		d.sealInflight.Wait()
	}()
	select {
	case <-waitDone:
	case <-ctx.Done():
	}

	d.stopOnce.Do(func() {
		close(d.stopCh)
	})
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

func (d *s3Driver) flushLoop(ctx context.Context) {
	defer close(d.doneCh)

	ticker := time.NewTicker(d.cfg.FlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-d.stopCh:
			d.drainSealed(ctx)
			d.flushPartial(ctx)
			return
		case <-ctx.Done():
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

func (d *s3Driver) drainSealed(ctx context.Context) {
	for {
		select {
		case sealed := <-d.sealCh:
			d.uploadBatch(ctx, sealed)
		default:
			return
		}
	}
}

func (d *s3Driver) flushPartial(ctx context.Context) {
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

func (d *s3Driver) uploadBatch(ctx context.Context, b *batch) {
	records := b.records[:b.len()]
	checkpoints := b.checkpoints[:b.len()]
	frames := b.frames[:b.len()]

	data, err := d.encoder.Encode(records)
	if err != nil {
		logging.L().ErrorContext(ctx, "s3 sink: encode failed",
			"error", err, "frames", len(records))
		d.nackAll(ctx, frames, err)
		d.recycleBatch(b)
		return
	}

	key := d.objectKey()

	_, err = d.client.PutObject(ctx, &s3svc.PutObjectInput{
		Bucket:      aws.String(d.cfg.Bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(data),
		ContentType: aws.String(d.encoder.ContentType()),
	})
	if err != nil {
		logging.L().ErrorContext(ctx, "s3 sink: upload failed",
			"error", err, "key", key, "frames", len(records))
		d.nackAll(ctx, frames, err)
		d.recycleBatch(b)
		return
	}

	d.ackAll(ctx, checkpoints)
	d.recycleBatch(b)
}

func (d *s3Driver) ackAll(ctx context.Context, checkpoints []*pb.CheckpointToken) {
	if d.ack == nil {
		return
	}
	for _, tok := range checkpoints {
		d.ack(ctx, tok)
	}
}

func (d *s3Driver) nackAll(ctx context.Context, frames []*pb.Frame, err error) {
	if d.nack == nil {
		return
	}
	for _, f := range frames {
		d.nack(ctx, f, err)
	}
}

func (d *s3Driver) objectKey() string {
	name := fmt.Sprintf("%d_data%s", time.Now().UnixNano(), d.cfg.FileSuffix)
	prefix := strings.TrimRight(d.cfg.Prefix, "/")
	if prefix == "" {
		return name
	}
	return prefix + "/" + name
}

func (d *s3Driver) recycleBatch(b *batch) {
	b.reset()
	d.pool.Put(b)
}

// errBadConfigType is returned when the registry hands the factory a config
// of the wrong type — should never happen at runtime if DecodeConfig is sane.
var errBadConfigType = errors.New("unexpected config type")
