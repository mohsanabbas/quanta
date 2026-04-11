package pipeline

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"time"

	pb "quanta/api/proto/v1"
	qerr "quanta/internal/errors"
	"quanta/internal/transform"
	"quanta/sink"
	"quanta/source"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type transformOutcome int8

const (
	outcomeEvents transformOutcome = iota

	outcomeDrop

	outcomeFailed

	outcomeAbort
)

type DeadLetterFn func(stage string, frame *pb.Frame, cause error)

type Runner struct {
	source source.Adapter
	sinks  []sink.Adapter
	stages []transformStage

	coord *AckCoordinator

	ackAwareSinks int
	dlqSink       sink.Adapter

	sourceErr chan error
}

type transformStage struct {
	name          string
	client        transform.Client
	timeout       time.Duration
	retryAttempts int
	retryBackoff  time.Duration
	errorSink     sink.Adapter
}

func NewRunner(coord *AckCoordinator) *Runner {
	if coord == nil {
		coord = NewAckCoordinator()
	}
	return &Runner{
		stages:    make([]transformStage, 0, 4),
		sinks:     make([]sink.Adapter, 0, 2),
		sourceErr: make(chan error, 1),
		coord:     coord,
	}
}

func (r *Runner) SetSource(s source.Adapter) {
	r.source = s
}

func (r *Runner) AddSink(s sink.Adapter) {
	if ackAware, ok := s.(sink.AckAware); ok {
		ackAware.BindAck(r.coord.Ack)
		r.ackAwareSinks++
	}
	if nackAware, ok := s.(sink.NackAware); ok {
		nackAware.BindNack(r.coord.Nack)
	}
	r.sinks = append(r.sinks, s)
}

func (r *Runner) SetDLQSink(s sink.Adapter) {
	r.coord.SetDLQSink(s)
	r.dlqSink = s
}

func (r *Runner) SetDeadLetter(fn DeadLetterFn) {
	r.coord.SetDeadLetter(fn)
}

func (r *Runner) AddTransformer(name string, c transform.Client, timeout time.Duration, attempts int, backoff time.Duration, errSink sink.Adapter) {
	r.stages = append(r.stages, transformStage{
		name:          name,
		client:        c,
		timeout:       timeout,
		retryAttempts: attempts,
		retryBackoff:  backoff,
		errorSink:     errSink,
	})
}

func (r *Runner) SubscribeAck(fn func(*pb.ConnectorAck)) {
	r.coord.Subscribe(fn)
}

func (r *Runner) Start(ctx context.Context) error {
	if r.source == nil {
		return qerr.Pipeline("start", errors.New("no source configured"))
	}
	go func() {
		err := r.source.Run(ctx, func(runCtx context.Context, frame *pb.Frame) error {
			return r.pushFrame(runCtx, frame)
		})
		if err != nil && ctx.Err() == nil {
			select {
			case r.sourceErr <- qerr.Pipeline("source-run", err):
			default:
				slog.Warn("engine: dropping source error; no receiver", "error", err)
			}
		}
	}()
	return nil
}

func (r *Runner) SourceErr() <-chan error {
	return r.sourceErr
}

func (r *Runner) Close(ctx context.Context) error {
	var errs []error
	for _, st := range r.stages {
		if err := st.client.Close(); err != nil {
			errs = append(errs, qerr.Transform(st.name, "close", err))
		}
		if st.errorSink != nil {
			if err := st.errorSink.Close(ctx); err != nil {
				errs = append(errs, qerr.Sink(st.name+"-error-sink", "close", err))
			}
		}
	}
	for _, s := range r.sinks {
		if err := s.Close(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	if r.dlqSink != nil {
		if err := r.dlqSink.Close(ctx); err != nil {
			errs = append(errs, qerr.Sink("dlq", "close", err))
		}
	}
	return errors.Join(errs...)
}

func (r *Runner) pushFrame(ctx context.Context, f *pb.Frame) error {
	frames := []*pb.Frame{f}

	for _, st := range r.stages {
		frames = r.runStage(ctx, st, frames)
	}

	if len(frames) == 0 {
		r.coord.CommitNow(f.Checkpoint)
		return nil
	}

	syncSinks := len(r.sinks) - r.ackAwareSinks
	refs := len(frames) * r.ackAwareSinks
	if syncSinks > 0 || r.ackAwareSinks == 0 {
		refs++
	}

	barrier := r.coord.Barrier(f.Checkpoint, refs)

	if err := r.publishAll(ctx, frames); err != nil {
		if r.coord.HasDLQ() {
			r.coord.Nack(ctx, f, err)
			return nil
		}
		barrier.Abort()
		return err
	}

	if syncSinks > 0 || r.ackAwareSinks == 0 {
		barrier.Complete()
	}

	return nil
}

func (r *Runner) runStage(ctx context.Context, st transformStage, in []*pb.Frame) []*pb.Frame {
	out := make([]*pb.Frame, 0, len(in))
	for _, f := range in {
		outcome, events, errEvents := r.callTransform(ctx, st, f)
		switch outcome {
		case outcomeEvents:
			out = append(out, toFrames(f, events)...)
		case outcomeDrop, outcomeFailed:
			// no-op: filtered or dead-lettered
		case outcomeAbort:
			// context cancelled, stop processing
		}
		if len(errEvents) > 0 {
			r.publishErrorEvents(ctx, st, f, errEvents)
		}
	}
	return out
}

func (r *Runner) callTransform(ctx context.Context, st transformStage, f *pb.Frame) (transformOutcome, []*pb.Event, []*pb.Event) {
	req := toRequest(f)
	req.PluginId = st.name

	for try := 0; ; try++ {
		if ctx.Err() != nil {
			return outcomeAbort, nil, nil
		}

		callCtx, cancel := r.stageContext(ctx, st.timeout)
		resp, err := st.client.Transform(callCtx, req)
		cancel()

		if err != nil {
			outcome, retry := r.handleTransportError(ctx, st, f, err, try)
			if retry {
				continue
			}
			return outcome, nil, nil
		}

		outcome, events, errEvents, retry := r.handleResponse(ctx, st, f, resp, try)
		if retry {
			continue
		}
		return outcome, events, errEvents
	}
}

func (r *Runner) handleTransportError(ctx context.Context, st transformStage, f *pb.Frame, err error, try int) (transformOutcome, bool) {
	isTimeout := errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
	if isTimeout && ctx.Err() != nil {
		return outcomeAbort, false
	}

	if isTimeout || isTransientGRPC(err) {
		switch r.retryOrExhaust(ctx, st, try) {
		case retryProceed:
			return 0, true
		case retryAbort:
			return outcomeAbort, false
		case retryExhaust:
		}
		return r.handlePermanentFailure(st, f, err), false
	}

	slog.Warn("transform permanent transport error",
		"stage", st.name,
		"code", status.Code(err).String(),
		"error", err,
	)
	return r.handlePermanentFailure(st, f, err), false
}

func (r *Runner) handleResponse(ctx context.Context, st transformStage, f *pb.Frame, resp *pb.TransformResponse, try int) (transformOutcome, []*pb.Event, []*pb.Event, bool) {
	switch resp.GetStatus() {
	case pb.Status_OK:
		return outcomeEvents, resp.GetEvents(), resp.GetErrorEvents(), false

	case pb.Status_DROP:
		slog.Debug("transform frame dropped by plugin",
			"stage", st.name,
		)
		return outcomeDrop, nil, nil, false

	case pb.Status_RETRY:
		switch r.retryOrExhaust(ctx, st, try) {
		case retryProceed:
			return 0, nil, nil, true
		case retryAbort:
			return outcomeAbort, nil, nil, false
		case retryExhaust:
		}
		return r.handlePermanentFailure(st, f,
			errors.New("plugin returned RETRY but retries exhausted")), nil, nil, false

	case pb.Status_ERROR:
		slog.Error("transform plugin returned permanent ERROR",
			"stage", st.name,
		)
		return r.handlePermanentFailure(st, f,
			errors.New("plugin returned permanent ERROR")), nil, nil, false

	default:
		slog.Error("transform unknown status from plugin",
			"stage", st.name,
			"status", resp.GetStatus().String(),
		)
		return r.handlePermanentFailure(st, f,
			errors.New("plugin returned unknown status")), nil, nil, false
	}
}

type retryVerdict int8

const (
	retryProceed retryVerdict = iota
	retryExhaust
	retryAbort
)

func (r *Runner) retryOrExhaust(ctx context.Context, st transformStage, try int) retryVerdict {
	if try >= st.retryAttempts {
		return retryExhaust
	}
	slog.Debug("transform retrying",
		"stage", st.name,
		"attempt", try+1,
		"max", st.retryAttempts,
	)
	if r.backoffOrCancel(ctx, st.retryBackoff) {
		return retryProceed
	}
	return retryAbort
}

func (r *Runner) handlePermanentFailure(st transformStage, f *pb.Frame, cause error) transformOutcome {
	r.coord.Fail(st.name, f, cause)
	return outcomeFailed
}

func isTransientGRPC(err error) bool {
	switch status.Code(err) {
	case codes.Unavailable,
		codes.DeadlineExceeded,
		codes.ResourceExhausted,
		codes.Aborted:
		return true
	default:
		return false
	}
}

func (r *Runner) backoffOrCancel(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
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

func (r *Runner) publishErrorEvents(ctx context.Context, st transformStage, orig *pb.Frame, events []*pb.Event) {
	if st.errorSink == nil {
		slog.Warn("transform plugin returned error_events but no error_sink configured",
			"stage", st.name,
			"count", len(events),
		)
		return
	}
	for _, frame := range toFrames(orig, events) {
		if err := st.errorSink.Publish(ctx, frame); err != nil {
			slog.Error("error_sink publish failed",
				"stage", st.name,
				"error", err,
			)
		}
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
