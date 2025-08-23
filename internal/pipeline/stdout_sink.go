// internal/pipeline/stdout_sink.go
package pipeline

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	pb "quanta/api/proto/v1"
)

/* -------------------------------------------------------------------------- */
/*  Debug knobs (filled by compiler.go)                                       */
/* -------------------------------------------------------------------------- */

type debugCfg struct {
	delay      time.Duration // artificial delay per frame
	counter    bool          // print global sequence #
	batchSize  int           // flush after N frames   (0 = off)
	flushEvery time.Duration // flush after this time  (0 = off)
}

/* -------------------------------------------------------------------------- */

type stdoutSink struct {
	ack   func(*pb.CheckpointToken) // injected by Runner
	debug debugCfg

	mu      sync.Mutex            // guards everything below
	pending []*pb.CheckpointToken // tokens awaiting flush
	timer   *time.Timer           // nil until first token arrives
}

var globalSeq uint64 // pretty-print counter

/* -------------------------------------------------------------------------- */

func (s *stdoutSink) Push(f *pb.Frame) error {
	/* optional delay – demo only */
	if d := s.debug.delay; d > 0 {
		time.Sleep(d)
	}

	/* print */
	seq := atomic.AddUint64(&globalSeq, 1)
	if s.debug.counter {
		fmt.Printf("[sink %06d] %s[%d]@%d\n",
			seq,
			f.Checkpoint.GetKafka().Topic,
			f.Checkpoint.GetKafka().Partition,
			f.Checkpoint.GetKafka().Offset)
	} else {
		fmt.Printf("[sink] %s[%d]@%d\n",
			f.Checkpoint.GetKafka().Topic,
			f.Checkpoint.GetKafka().Partition,
			f.Checkpoint.GetKafka().Offset)
	}

	/* ACK handling */
	if s.ack == nil {
		return nil
	}

	s.mu.Lock()
	s.pending = append(s.pending, f.Checkpoint)

	/* size-based flush */
	if s.debug.batchSize > 0 && len(s.pending) >= s.debug.batchSize {
		s.flushLocked()
		s.mu.Unlock()
		return nil
	}

	/* arm (or re-arm) the timer for time-based flush */
	if s.debug.flushEvery > 0 {
		if s.timer == nil {
			s.timer = time.AfterFunc(s.debug.flushEvery, s.onTimer)
		} else {
			s.timer.Reset(s.debug.flushEvery)
		}
	}
	s.mu.Unlock()
	return nil
}

/* -------------------------------------------------------------------------- */
/*  timer callback – runs in its own goroutine                                */
/* -------------------------------------------------------------------------- */

func (s *stdoutSink) onTimer() {
	s.mu.Lock()
	s.flushLocked()
	s.timer = nil // let the next Push arm it again
	s.mu.Unlock()
}

/* -------------------------------------------------------------------------- */

func (s *stdoutSink) flushLocked() {
	if len(s.pending) == 0 {
		return
	}
	for _, t := range s.pending {
		s.ack(t) // Driver.OnAck → bp.Release(1)
	}
	s.pending = s.pending[:0]
}
