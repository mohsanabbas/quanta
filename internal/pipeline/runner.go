package pipeline

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"
	pb "quanta/api/proto/v1"
	"quanta/internal/transform"
	"quanta/sink"
	"quanta/source/kafka"
)

type Runner struct {
	source kafka.Adapter
	sinks  []sink.Adapter

	stages []transformStage

	mu   sync.Mutex
	subs []func(*pb.ConnectorAck)
}

type transformStage struct {
	name          string
	client        transform.Client
	timeout       time.Duration
	retryAttempts int
	retryBackoff  time.Duration
}

func NewRunner() *Runner { return &Runner{} }

func (r *Runner) AddSink(s sink.Adapter)    { r.sinks = append(r.sinks, s) }
func (r *Runner) SetSource(s kafka.Adapter) { r.source = s }

func (r *Runner) AddTransformer(name string, c transform.Client, timeout time.Duration, attempts int, backoff time.Duration) {
	r.stages = append(r.stages, transformStage{name: name, client: c, timeout: timeout, retryAttempts: attempts, retryBackoff: backoff})
}

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

func toRequest(f *pb.Frame) *pb.TransformRequest {
	md := &pb.EventMetadata{}
	if f.Ts != nil {
		md.TimestampMs = f.Ts.AsTime().UnixMilli()
	}
	if len(f.Headers) > 0 {
		md.Headers = make(map[string]string, len(f.Headers))
		for k, v := range f.Headers {
			md.Headers[k] = string(v)
		}
	}
	if k := f.GetCheckpoint().GetKafka(); k != nil {
		md.SourcePartition = fmt.Sprintf("%d", k.Partition)
		md.SourceOffset = fmt.Sprintf("%d", k.Offset)
		if md.Attributes == nil {
			md.Attributes = map[string]string{}
		}
		md.Attributes["source.topic"] = k.Topic
	}
	return &pb.TransformRequest{
		PipelineId: "",
		PluginId:   "",
		Payload:    f.Value,
		Metadata:   md,
		BatchMode:  false,
	}
}

func toFrames(orig *pb.Frame, events []*pb.Event) []*pb.Frame {
	if len(events) == 0 {
		return nil
	}
	out := make([]*pb.Frame, 0, len(events))
	for _, ev := range events {
		g := &pb.Frame{
			Key:        orig.Key,
			Value:      ev.GetValue(),
			Headers:    nil,
			Ts:         orig.Ts,
			Checkpoint: orig.Checkpoint,
		}
		if md := ev.GetMetadata(); md != nil {
			if md.TimestampMs > 0 {

				g.Ts = timestamppb.New(time.UnixMilli(md.TimestampMs))
			}
			if len(md.Headers) > 0 {
				g.Headers = make(map[string][]byte, len(md.Headers))
				for k, v := range md.Headers {
					g.Headers[k] = []byte(v)
				}
			}
		}
		out = append(out, g)
	}
	return out
}

func (r *Runner) pushFrame(f *pb.Frame) error {
	frames := []*pb.Frame{f}

	for _, st := range r.stages {
		next := make([]*pb.Frame, 0)
		for _, in := range frames {
			var (
				resp *pb.TransformResponse
				err  error
			)

			req := toRequest(in)

			req.PluginId = st.name

			attempts := st.retryAttempts
			for try := 0; ; try++ {
				ctx := context.Background()
				var cancel context.CancelFunc
				if st.timeout > 0 {
					ctx, cancel = context.WithTimeout(ctx, st.timeout)
				}
				resp, err = st.client.Transform(ctx, req)
				if cancel != nil {
					cancel()
				}

				if err != nil {
					if try < attempts {
						time.Sleep(st.retryBackoff)
						continue
					}

					r.Ack(in.Checkpoint)
					resp = nil
					break
				}

				switch resp.GetStatus() {
				case pb.Status_OK:

				case pb.Status_DROP:

					r.Ack(in.Checkpoint)
					resp.Events = nil

				default:
					if try < attempts {
						time.Sleep(st.retryBackoff)
						continue
					}

					r.Ack(in.Checkpoint)
					resp.Events = nil
				}
				break
			}

			if resp == nil || len(resp.GetEvents()) == 0 {
				continue
			}

			outs := toFrames(in, resp.GetEvents())
			next = append(next, outs...)
		}
		frames = next
		if len(frames) == 0 {

			return nil
		}
	}

	for _, fr := range frames {
		for _, s := range r.sinks {
			if err := s.Push(fr); err != nil {
				return err
			}
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

func (r *Runner) Close() error {

	for _, st := range r.stages {
		_ = st.client.Close()
	}

	for _, s := range r.sinks {
		_ = s.Close()
	}
	return nil
}
