package stdout

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	pb "quanta/api/proto/v1"
	"quanta/internal/logging"
	"quanta/sink"
)

type Config struct {
	DelayMS       int  `yaml:"delay_ms"`
	PrintCounter  bool `yaml:"print_counter"`
	BatchSize     int  `yaml:"ack_batch_size"`
	FlushMS       int  `yaml:"ack_flush_ms"`
	PrintValue    bool `yaml:"print_value"`
	ValueMaxBytes int  `yaml:"value_max_bytes"`
}

type driver struct {
	cfg Config
	ack sink.EmitFn

	mu      sync.Mutex
	pending []*pb.CheckpointToken
	timer   *time.Timer
}

var seq uint64

func (d *driver) Configure(ctx context.Context, raw any) error {
	c, ok := raw.(Config)
	if !ok {
		got := reflect.TypeOf(raw).String()
		logging.L().With("component", "sink.stdout").Error("invalid config type", "got", got)
		return errors.New("stdout-sink: invalid config type")
	}
	if c.ValueMaxBytes <= 0 {
		c.ValueMaxBytes = 120
	}
	d.cfg = c
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
		attrs := []any{
			"topic", f.Checkpoint.GetKafka().Topic,
			"partition", f.Checkpoint.GetKafka().Partition,
			"offset", f.Checkpoint.GetKafka().Offset,
			"seq", atomic.AddUint64(&seq, 1),
		}
		if d.cfg.PrintValue && len(f.Value) > 0 {
			max := d.cfg.ValueMaxBytes
			if max > len(f.Value) {
				max = len(f.Value)
			}
			attrs = append(attrs, "value", string(f.Value[:max]))
		}
		logging.L().Info("sink stdout", attrs...)
	}

	d.mu.Lock()
	d.pending = append(d.pending, f.Checkpoint)

	if d.cfg.BatchSize > 0 && len(d.pending) >= d.cfg.BatchSize {
		d.flushLocked()
		d.mu.Unlock()
		return nil
	}

	if d.cfg.FlushMS > 0 && d.timer == nil {
		d.timer = time.AfterFunc(
			time.Duration(d.cfg.FlushMS)*time.Millisecond,
			d.timerFlush,
		)
	}
	d.mu.Unlock()
	return nil
}

func (d *driver) Close(ctx context.Context) error {
	d.mu.Lock()
	d.flushLocked()
	d.mu.Unlock()
	return nil
}

func (d *driver) BindAck(fn sink.EmitFn) { d.ack = fn }

func (d *driver) timerFlush() {
	d.mu.Lock()
	d.flushLocked()
	d.mu.Unlock()
}

func (d *driver) flushLocked() {
	if len(d.pending) == 0 || d.ack == nil {
		d.stopTimerLocked()
		return
	}
	for _, t := range d.pending {
		d.ack(t)
	}
	d.pending = d.pending[:0]
	d.stopTimerLocked()
}

func (d *driver) stopTimerLocked() {
	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
}

func init() {
	sink.Register(sink.Registration{
		Name:        "stdout",
		New:         func() sink.Adapter { return &driver{} },
		ConfigProto: func() any { return Config{} },
	})
}
