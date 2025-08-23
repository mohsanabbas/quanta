// quanta/sink/stdout/driver.go
package stdout

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	pb "quanta/api/proto/v1"
	"quanta/sink"
)

/* ────────── public YAML config ────────── */
type Config struct {
	DelayMS      int  `yaml:"delay_ms"`       // artificial per-frame delay
	PrintCounter bool `yaml:"print_counter"`  // prepend seq#
	BatchSize    int  `yaml:"ack_batch_size"` // 0 = disabled
	FlushMS      int  `yaml:"ack_flush_ms"`   // 0 = disabled
}

/* ────────── driver ────────── */
type driver struct {
	cfg Config
	ack sink.EmitFn

	mu      sync.Mutex // guards pending+timer
	pending []*pb.CheckpointToken
	timer   *time.Timer // nil → no timer armed
}

var seq uint64

/* ────────── sink.Adapter ────────── */
func (d *driver) Configure(raw any) error {
	c, ok := raw.(Config)
	if !ok {
		return fmt.Errorf("stdout-sink: expected Config, got %T", raw)
	}
	d.cfg = c
	return nil
}

func (d *driver) Push(f *pb.Frame) error {
	if d.cfg.DelayMS > 0 {
		time.Sleep(time.Duration(d.cfg.DelayMS) * time.Millisecond)
	}

	if d.cfg.PrintCounter {
		fmt.Printf("[sink %06d] %s[%d]@%d\n",
			atomic.AddUint64(&seq, 1),
			f.Checkpoint.GetKafka().Topic,
			f.Checkpoint.GetKafka().Partition,
			f.Checkpoint.GetKafka().Offset)
	}

	d.mu.Lock()
	d.pending = append(d.pending, f.Checkpoint)

	/* 1. flush on batch size */
	if d.cfg.BatchSize > 0 && len(d.pending) >= d.cfg.BatchSize {
		d.flushLocked()
		d.mu.Unlock()
		return nil
	}

	/* 2. (re)-arm the one-shot timer if needed */
	if d.cfg.FlushMS > 0 && d.timer == nil {
		d.timer = time.AfterFunc(
			time.Duration(d.cfg.FlushMS)*time.Millisecond,
			d.timerFlush,
		)
	}
	d.mu.Unlock()
	return nil
}

func (d *driver) Close() error {
	d.mu.Lock()
	d.flushLocked()
	d.mu.Unlock()
	return nil
}

/* ────────── sink.AckAware ────────── */
func (d *driver) BindAck(fn sink.EmitFn) { d.ack = fn }

/* ────────── internals ────────── */

// called by the background timer goroutine
func (d *driver) timerFlush() {
	d.mu.Lock()
	d.flushLocked()
	d.mu.Unlock()
}

// must be called with d.mu *held*
func (d *driver) flushLocked() {
	if len(d.pending) == 0 || d.ack == nil {
		d.stopTimerLocked()
		return
	}
	for _, t := range d.pending {
		d.ack(t)
	}
	d.pending = d.pending[:0]
	d.stopTimerLocked() // re-arm on next Push if needed
}

func (d *driver) stopTimerLocked() {
	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
}

/* ────────── auto-register ────────── */
func init() {
	sink.Register("stdout", func() sink.Adapter { return &driver{} })
}
