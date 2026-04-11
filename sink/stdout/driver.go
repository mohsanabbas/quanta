package stdout

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	pb "quanta/api/proto/v1"
	qerr "quanta/internal/errors"
	"quanta/internal/logging"
	"quanta/sink"
)

type Config struct {
	DelayMS       int  `yaml:"delay_ms"`
	PrintCounter  bool `yaml:"print_counter"`
	BatchSize     int  `yaml:"ack_batch_size"`
	PrintValue    bool `yaml:"print_value"`
	ValueMaxBytes int  `yaml:"value_max_bytes"`
}

type driver struct {
	cfg     Config
	ack     sink.EmitFn
	mu      sync.Mutex
	pending []*pb.CheckpointToken
}

var (
	_ sink.Adapter  = (*driver)(nil)
	_ sink.AckAware = (*driver)(nil)
)

var seq uint64

func (d *driver) Configure(_ context.Context, raw any) error {
	cfg, ok := raw.(Config)
	if !ok {
		if p, ok2 := raw.(*Config); ok2 && p != nil {
			cfg = *p
		} else {
			got := reflect.TypeOf(raw).String()
			logging.L().With("component", "sink.stdout").Error("invalid config type", "got", got)
			return qerr.Sink("stdout", "configure", errors.New("invalid config type"))
		}
	}
	if cfg.ValueMaxBytes <= 0 {
		cfg.ValueMaxBytes = 120
	}
	d.cfg = cfg
	return nil
}

func (d *driver) Publish(ctx context.Context, f *pb.Frame) error {
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

func (d *driver) Close(ctx context.Context) error {
	d.flush(ctx)
	return nil
}

func (d *driver) BindAck(fn sink.EmitFn) { d.ack = fn }

func (d *driver) flush(ctx context.Context) {
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
