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

func (d *driver) Configure(_ context.Context, raw any) error {
	cfg, ok := raw.(Config)
	if !ok {
		// also accept a pointer to Config
		if p, ok2 := raw.(*Config); ok2 && p != nil {
			cfg = *p
		} else {
			got := reflect.TypeOf(raw).String()
			logging.L().With("component", "sink.stdout").Error("invalid config type", "got", got)
			return errors.New("stdout-sink: invalid config type")
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

	if d.cfg.BatchSize > 0 && len(d.pending) >= d.cfg.BatchSize {
		d.flushLocked()
		d.mu.Unlock()
		return nil
	}

	if d.cfg.FlushMS > 0 && d.timer == nil {
		d.timer = time.AfterFunc(time.Duration(d.cfg.FlushMS)*time.Millisecond, d.timerFlush)
	}
	d.mu.Unlock()
	return nil
}

func (d *driver) Close(_ context.Context) error {
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
