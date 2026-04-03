package pipeline

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"time"

	pb "quanta/api/proto/v1"
	qerr "quanta/internal/errors"
	"quanta/internal/transform"
	"quanta/sink"
	"quanta/source"

	"google.golang.org/protobuf/types/known/timestamppb"
)

type Runner struct {
	source source.Adapter
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

func NewRunner() *Runner {
	return &Runner{
		stages: make([]transformStage, 0, 4),
		sinks:  make([]sink.Adapter, 0, 2),
	}
}

func (r *Runner) SetSource(s source.Adapter) { r.source = s }

func (r *Runner) AddSink(s sink.Adapter) { r.sinks = append(r.sinks, s) }

func (r *Runner) AddTransformer(name string, c transform.Client, timeout time.Duration, attempts int, backoff time.Duration) {
	r.stages = append(r.stages, transformStage{
		name:          name,
		client:        c,
		timeout:       timeout,
		retryAttempts: attempts,
		retryBackoff:  backoff,
	})
}

func (r *Runner) SubscribeAck(fn func(*pb.ConnectorAck)) {
	r.mu.Lock()
	r.subs = append(r.subs, fn)
	r.mu.Unlock()
}

func (r *Runner) Ack(tok *pb.CheckpointToken) {
	ack := &pb.ConnectorAck{Checkpoint: tok}

	r.mu.Lock()
	handlers := make([]func(*pb.ConnectorAck), len(r.subs))
	copy(handlers, r.subs)
	r.mu.Unlock()

	for _, fn := range handlers {
		fn(ack)
	}
}

func (r *Runner) Start(ctx context.Context) error {
	if r.source == nil {
		return qerr.Pipeline("start", errors.New("no source configured"))
	}
	go func() {
		_ = r.source.Run(ctx, func(runCtx context.Context, frame *pb.Frame) error {
			return r.pushFrame(runCtx, frame)
		})
	}()
	return nil
}

func (r *Runner) Close(ctx context.Context) error {
	for _, st := range r.stages {
		_ = st.client.Close()
	}
	for _, s := range r.sinks {
		_ = s.Close(ctx)
	}
	return nil
}

func (r *Runner) pushFrame(ctx context.Context, f *pb.Frame) error {
	frames := []*pb.Frame{f}

	for _, st := range r.stages {
		frames = r.runStage(ctx, st, frames)
	}

	if len(frames) == 0 {
		r.Ack(f.Checkpoint)
		return nil
	}

	if err := r.publishAll(ctx, frames); err != nil {
		return err
	}

	r.Ack(f.Checkpoint)
	return nil
}

func (r *Runner) runStage(ctx context.Context, st transformStage, in []*pb.Frame) []*pb.Frame {
	out := make([]*pb.Frame, 0, len(in))
	for _, f := range in {
		events := r.callTransform(ctx, st, f)
		if events == nil {
			continue
		}
		out = append(out, toFrames(f, events)...)
	}
	return out
}

func (r *Runner) callTransform(ctx context.Context, st transformStage, f *pb.Frame) []*pb.Event {
	req := toRequest(f)
	req.PluginId = st.name

	for try := 0; ; try++ {
		callCtx, cancel := r.stageContext(ctx, st.timeout)
		resp, err := st.client.Transform(callCtx, req)
		cancel()

		if err != nil {
			if try < st.retryAttempts {
				time.Sleep(st.retryBackoff)
				continue
			}
			r.Ack(f.Checkpoint)
			return nil
		}

		switch resp.GetStatus() {
		case pb.Status_OK:
			return resp.GetEvents()
		case pb.Status_DROP:
			r.Ack(f.Checkpoint)
			return nil
		case pb.Status_RETRY, pb.Status_ERROR:
			if try < st.retryAttempts {
				time.Sleep(st.retryBackoff)
				continue
			}
			r.Ack(f.Checkpoint)
			return nil
		default:
			if try < st.retryAttempts {
				time.Sleep(st.retryBackoff)
				continue
			}
			r.Ack(f.Checkpoint)
			return nil
		}
	}
}

func (r *Runner) stageContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout > 0 {
		return context.WithTimeout(ctx, timeout)
	}
	return ctx, func() {}
}

func (r *Runner) publishAll(ctx context.Context, frames []*pb.Frame) error {
	for _, f := range frames {
		for _, s := range r.sinks {
			if err := s.Publish(ctx, f); err != nil {
				return qerr.Sink("", "publish", err)
			}
		}
	}
	return nil
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
		md.SourcePartition = strconv.FormatInt(int64(k.Partition), 10)
		md.SourceOffset = strconv.FormatInt(k.Offset, 10)
		if md.Attributes == nil {
			md.Attributes = map[string]string{}
		}
		md.Attributes["source.topic"] = k.Topic
	}
	return &pb.TransformRequest{
		Payload:  f.Value,
		Metadata: md,
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
			if keyAttr, ok := md.Attributes["sink.key"]; ok && keyAttr != "" {
				g.Key = []byte(keyAttr)
			}
		}
		out = append(out, g)
	}
	return out
}
