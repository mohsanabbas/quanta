// Package stdout — debug/test sink that prints frames to stdout.
package stdout

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	pb "quanta/api/proto/v1"
	"quanta/internal/logging"
	"quanta/sink"
)

// Config controls how frames are printed and acknowledged.
type Config struct {
	DelayMS       int  `yaml:"delay_ms"`
	PrintCounter  bool `yaml:"print_counter"`
	BatchSize     int  `yaml:"ack_batch_size"`
	PrintValue    bool `yaml:"print_value"`
	ValueMaxBytes int  `yaml:"value_max_bytes"`
}

type stdoutDriver struct {
	cfg     Config
	ack     sink.EmitFn
	mu      sync.Mutex
	pending []*pb.CheckpointToken
}

var _ sink.Adapter = (*stdoutDriver)(nil)

var seq uint64

func newStdoutDriver(cfg Config, opts sink.BuildOptions) *stdoutDriver {
	if cfg.ValueMaxBytes <= 0 {
		cfg.ValueMaxBytes = 120
	}
	return &stdoutDriver{cfg: cfg, ack: opts.Ack}
}

func (d *stdoutDriver) Name() string { return "stdout" }

func (d *stdoutDriver) Caps() sink.Capabilities {
	return sink.Capabilities{AckAware: true}
}

func (d *stdoutDriver) Publish(ctx context.Context, f *pb.Frame) error {
	if d.cfg.DelayMS > 0 {
		delay := time.Duration(d.cfg.DelayMS) * time.Millisecond
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	if d.cfg.PrintCounter {
		attrs := []any{"seq", atomic.AddUint64(&seq, 1)}
		if k := f.GetCheckpoint().GetKafka(); k != nil {
			attrs = append(attrs, "topic", k.Topic, "partition", k.Partition, "offset", k.Offset)
		}
		if d.cfg.PrintValue && len(f.Value) > 0 {
			maxBytes := d.cfg.ValueMaxBytes
			if maxBytes > len(f.Value) {
				maxBytes = len(f.Value)
			}
			attrs = append(attrs, "value", string(f.Value[:maxBytes]))
		}
		logging.L().Info("sink stdout", attrs...)
	}

	d.mu.Lock()
	d.pending = append(d.pending, f.Checkpoint)
	shouldFlush := d.cfg.BatchSize > 0 && len(d.pending) >= d.cfg.BatchSize
	d.mu.Unlock()

	if shouldFlush {
		d.flush(ctx)
	}
	return nil
}

func (d *stdoutDriver) Close(ctx context.Context) error {
	d.flush(ctx)
	return nil
}

func (d *stdoutDriver) flush(ctx context.Context) {
	d.mu.Lock()
	if len(d.pending) == 0 {
		d.mu.Unlock()
		return
	}
	tokens := d.pending
	d.pending = nil
	d.mu.Unlock()

	if d.ack == nil {
		return
	}
	for _, tok := range tokens {
		d.ack(ctx, tok)
	}
}
