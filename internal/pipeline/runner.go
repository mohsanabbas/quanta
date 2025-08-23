package pipeline

import (
	"context"
	"errors"
	"sync"

	pb "quanta/api/proto/v1"
	"quanta/sink"
	"quanta/source/kafka"
)

type Runner struct {
	source kafka.Adapter
	sinks  []sink.Adapter

	mu   sync.Mutex
	subs []func(*pb.ConnectorAck)
}

func NewRunner() *Runner { return &Runner{} }

func (r *Runner) AddSink(s sink.Adapter)    { r.sinks = append(r.sinks, s) }
func (r *Runner) SetSource(s kafka.Adapter) { r.source = s }

func (r *Runner) SubscribeAck(fn func(*pb.ConnectorAck)) {
	r.mu.Lock()
	r.subs = append(r.subs, fn)
	r.mu.Unlock()
}

func (r *Runner) Ack(tok *pb.CheckpointToken) {
	ack := &pb.ConnectorAck{Checkpoint: tok}

	r.mu.Lock()
	handlers := append([]func(*pb.ConnectorAck){}, r.subs...)
	r.mu.Unlock()

	for _, fn := range handlers {
		fn(ack)
	}
}

/*──────── frame routing ───────*/
func (r *Runner) pushFrame(f *pb.Frame) error {
	for _, s := range r.sinks {
		if err := s.Push(f); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runner) Start(ctx context.Context) error {
	if r.source == nil {
		return errors.New("runner: no source configured")
	}
	go func() { _ = r.source.Run(ctx, r.pushFrame) }()
	return nil
}
