package pipeline

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	pb "quanta/api/proto/v1"
	qerr "quanta/internal/errors"
	"quanta/sink"
	"quanta/source"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeTransform struct {
	calls int32
	mode  string
}

func (f *fakeTransform) Metadata(context.Context) (*pb.MetadataResponse, error) {
	return &pb.MetadataResponse{}, nil
}

func (f *fakeTransform) Health(context.Context) (*pb.HealthResponse, error) {
	return &pb.HealthResponse{Ok: true}, nil
}

func (f *fakeTransform) Close() error { return nil }
func (f *fakeTransform) Transform(_ context.Context, req *pb.TransformRequest) (*pb.TransformResponse, error) {
	c := atomic.AddInt32(&f.calls, 1)
	switch f.mode {
	case "ok":
		return &pb.TransformResponse{Status: pb.Status_OK, Events: []*pb.Event{{Value: append([]byte{}, req.Payload...)}}}, nil
	case "drop":
		return &pb.TransformResponse{Status: pb.Status_DROP}, nil
	case "errorThenOK":
		if c == 1 {
			return &pb.TransformResponse{Status: pb.Status_RETRY}, nil
		}
		return &pb.TransformResponse{Status: pb.Status_OK, Events: []*pb.Event{{Value: append([]byte{}, req.Payload...)}}}, nil
	case "fanout2":
		return &pb.TransformResponse{Status: pb.Status_OK, Events: []*pb.Event{{Value: append([]byte{}, req.Payload...)}, {Value: append([]byte{}, req.Payload...)}}}, nil
	case "permanent_error":
		return &pb.TransformResponse{Status: pb.Status_ERROR}, nil
	case "retry_always":
		return &pb.TransformResponse{Status: pb.Status_RETRY}, nil
	default:
		return &pb.TransformResponse{Status: pb.Status_OK, Events: []*pb.Event{{Value: append([]byte{}, req.Payload...)}}}, nil
	}
}

type grpcErrTransform struct {
	fakeTransform
	code      codes.Code
	callCount int32
	succeedAt int32
	// firstCall, when non-nil, is closed exactly once after Transform is
	// invoked for the first time. Tests use it as a deterministic gate so
	// they can react to the call without sleeping for an arbitrary duration.
	firstCall chan struct{}
}

func (g *grpcErrTransform) Transform(_ context.Context, req *pb.TransformRequest) (*pb.TransformResponse, error) {
	c := atomic.AddInt32(&g.callCount, 1)
	if c == 1 && g.firstCall != nil {
		close(g.firstCall)
	}
	if g.succeedAt > 0 && c >= g.succeedAt {
		return &pb.TransformResponse{Status: pb.Status_OK, Events: []*pb.Event{{Value: append([]byte{}, req.Payload...)}}}, nil
	}
	return nil, status.Error(g.code, "simulated gRPC error")
}

type failTransformClose struct {
	fakeTransform
	err error
}

func (f *failTransformClose) Close() error { return f.err }

type captureSink struct {
	mu     sync.Mutex
	pushed []*pb.Frame
	ackFn  sink.EmitFn
}

func (c *captureSink) Name() string            { return "capture" }
func (c *captureSink) Caps() sink.Capabilities { return sink.Capabilities{AckAware: true} }
func (c *captureSink) Publish(_ context.Context, f *pb.Frame) error {
	c.mu.Lock()
	c.pushed = append(c.pushed, f)
	c.mu.Unlock()
	if c.ackFn != nil {
		c.ackFn(context.Background(), f.Checkpoint)
	}
	return nil
}
func (c *captureSink) Close(context.Context) error { return nil }

// captureFor returns a captureSink wired to the runner's coordinator. Tests
// previously relied on AddSink to bind the ack callback; now that sinks are
// constructed via factories with BuildOptions, fixtures inject the callback
// directly.
func captureFor(coord *AckCoordinator) *captureSink {
	return &captureSink{ackFn: coord.Ack}
}

type failSink struct{ err error }

func (f *failSink) Name() string                             { return "fail" }
func (f *failSink) Caps() sink.Capabilities                  { return sink.Capabilities{} }
func (f *failSink) Publish(context.Context, *pb.Frame) error { return nil }
func (f *failSink) Close(context.Context) error              { return f.err }

type fakeSource struct {
	runErr error
	block  bool
}

func (s *fakeSource) Run(ctx context.Context, _ source.EmitFunc) error {
	if s.block {
		<-ctx.Done()
		return ctx.Err()
	}
	return s.runErr
}
func (s *fakeSource) OnAck(*pb.ConnectorAck)      {}
func (s *fakeSource) Close(context.Context) error { return nil }

type deadLetterCapture struct {
	mu      sync.Mutex
	entries []dlEntry
}

type dlEntry struct {
	stage string
	frame *pb.Frame
	cause error
}

func (d *deadLetterCapture) fn(stage string, frame *pb.Frame, cause error) error {
	d.mu.Lock()
	d.entries = append(d.entries, dlEntry{stage: stage, frame: frame, cause: cause})
	d.mu.Unlock()
	return nil
}

func (d *deadLetterCapture) len() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.entries)
}

func (d *deadLetterCapture) get(i int) dlEntry {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.entries[i]
}

func makeFrame() *pb.Frame {
	return &pb.Frame{
		Value: []byte("hello"),
		Checkpoint: &pb.CheckpointToken{Kind: &pb.CheckpointToken_Kafka{
			Kafka: &pb.KafkaOffset{Topic: "t", Partition: 1, Offset: 42},
		}},
	}
}

type ackCounter struct {
	mu    sync.Mutex
	count int
}

func (a *ackCounter) handler(ack *pb.ConnectorAck) {
	a.mu.Lock()
	a.count++
	a.mu.Unlock()
}

func (a *ackCounter) get() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.count
}

func newTestRunner() *Runner {
	return NewRunner(NewAckCoordinator())
}

func TestRunner_PushFrame(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		mode       string
		retries    int
		wantPushed int
		wantValue  string
	}{
		{
			name:       "transformer_ok_forwards_to_sink",
			mode:       "ok",
			retries:    0,
			wantPushed: 1,
			wantValue:  "hello",
		},
		{
			name:       "transformer_drop_acks_no_push",
			mode:       "drop",
			retries:    0,
			wantPushed: 0,
		},
		{
			name:       "transformer_retry_then_ok",
			mode:       "errorThenOK",
			retries:    1,
			wantPushed: 1,
			wantValue:  "hello",
		},
		{
			name:       "multi_stage_fanout",
			mode:       "fanout2",
			retries:    0,
			wantPushed: 2,
			wantValue:  "hello",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := newTestRunner()
			fake := &fakeTransform{mode: tt.mode}
			r.AddTransformer("t1", fake, 100*time.Millisecond, tt.retries, 1*time.Millisecond, nil)

			if tt.mode == "fanout2" {
				r.AddTransformer("t2", &fakeTransform{mode: "ok"}, 100*time.Millisecond, 0, 0, nil)
			}

			cs := captureFor(r.coord)
			r.AddSink(cs)

			if err := r.pushFrame(context.Background(), makeFrame()); err != nil {
				t.Fatalf("pushFrame: %v", err)
			}

			cs.mu.Lock()
			got := len(cs.pushed)
			cs.mu.Unlock()

			if got != tt.wantPushed {
				t.Fatalf("pushed frames: got %d, want %d", got, tt.wantPushed)
			}
			if tt.wantPushed > 0 && tt.wantValue != "" {
				cs.mu.Lock()
				val := string(cs.pushed[0].Value)
				cs.mu.Unlock()
				if val != tt.wantValue {
					t.Fatalf("pushed value: got %q, want %q", val, tt.wantValue)
				}
			}
		})
	}
}

func TestCallTransform_ErrorClassification(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		client         func() *grpcErrTransform
		retries        int
		wantOutcome    transformOutcome
		wantRetryCalls int32
		wantDL         bool
	}{
		{
			name:           "transient_unavailable_retried_then_dead_letter",
			client:         func() *grpcErrTransform { return &grpcErrTransform{code: codes.Unavailable} },
			retries:        2,
			wantOutcome:    outcomeFailed,
			wantRetryCalls: 3,
			wantDL:         true,
		},
		{
			name:           "transient_resource_exhausted_retried",
			client:         func() *grpcErrTransform { return &grpcErrTransform{code: codes.ResourceExhausted} },
			retries:        1,
			wantOutcome:    outcomeFailed,
			wantRetryCalls: 2,
			wantDL:         true,
		},
		{
			name:           "transient_succeeds_on_retry",
			client:         func() *grpcErrTransform { return &grpcErrTransform{code: codes.Unavailable, succeedAt: 2} },
			retries:        3,
			wantOutcome:    outcomeEvents,
			wantRetryCalls: 2,
			wantDL:         false,
		},
		{
			name:           "permanent_invalid_argument_never_retried",
			client:         func() *grpcErrTransform { return &grpcErrTransform{code: codes.InvalidArgument} },
			retries:        5,
			wantOutcome:    outcomeFailed,
			wantRetryCalls: 1,
			wantDL:         true,
		},
		{
			name:           "permanent_unimplemented_never_retried",
			client:         func() *grpcErrTransform { return &grpcErrTransform{code: codes.Unimplemented} },
			retries:        3,
			wantOutcome:    outcomeFailed,
			wantRetryCalls: 1,
			wantDL:         true,
		},
		{
			name:           "permanent_permission_denied_never_retried",
			client:         func() *grpcErrTransform { return &grpcErrTransform{code: codes.PermissionDenied} },
			retries:        3,
			wantOutcome:    outcomeFailed,
			wantRetryCalls: 1,
			wantDL:         true,
		},
		{
			name:           "permanent_internal_never_retried",
			client:         func() *grpcErrTransform { return &grpcErrTransform{code: codes.Internal} },
			retries:        3,
			wantOutcome:    outcomeFailed,
			wantRetryCalls: 1,
			wantDL:         true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cli := tt.client()
			r := newTestRunner()
			dl := &deadLetterCapture{}
			r.coord.SetDeadLetter(dl.fn)
			r.AddTransformer("test", cli, 50*time.Millisecond, tt.retries, 1*time.Millisecond, nil)
			r.AddSink(captureFor(r.coord))

			outcome, _, _ := r.callTransform(context.Background(),
				r.stages[0], makeFrame())

			if outcome != tt.wantOutcome {
				t.Fatalf("outcome: got %d, want %d", outcome, tt.wantOutcome)
			}

			calls := atomic.LoadInt32(&cli.callCount)
			if calls != tt.wantRetryCalls {
				t.Fatalf("Transform calls: got %d, want %d", calls, tt.wantRetryCalls)
			}

			if tt.wantDL {
				if dl.len() != 1 {
					t.Fatalf("dead-letter entries: got %d, want 1", dl.len())
				}
			} else {
				if dl.len() != 0 {
					t.Fatalf("dead-letter entries: got %d, want 0", dl.len())
				}
			}
		})
	}
}

func TestCallTransform_StatusClassification(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		mode        string
		retries     int
		wantOutcome transformOutcome
		wantCalls   int32
		wantDL      bool
	}{
		{
			name:        "status_ok_no_retry",
			mode:        "ok",
			retries:     3,
			wantOutcome: outcomeEvents,
			wantCalls:   1,
		},
		{
			name:        "status_drop_no_retry_no_dead_letter",
			mode:        "drop",
			retries:     3,
			wantOutcome: outcomeDrop,
			wantCalls:   1,
		},
		{
			name:        "status_error_permanent_never_retried",
			mode:        "permanent_error",
			retries:     5,
			wantOutcome: outcomeFailed,
			wantCalls:   1,
			wantDL:      true,
		},
		{
			name:        "status_retry_retried_then_dead_letter",
			mode:        "retry_always",
			retries:     2,
			wantOutcome: outcomeFailed,
			wantCalls:   3,
			wantDL:      true,
		},
		{
			name:        "status_retry_succeeds_on_second_attempt",
			mode:        "errorThenOK",
			retries:     2,
			wantOutcome: outcomeEvents,
			wantCalls:   2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fake := &fakeTransform{mode: tt.mode}
			r := newTestRunner()
			dl := &deadLetterCapture{}
			r.coord.SetDeadLetter(dl.fn)
			r.AddTransformer("test", fake, 100*time.Millisecond, tt.retries, 1*time.Millisecond, nil)
			r.AddSink(captureFor(r.coord))

			outcome, _, _ := r.callTransform(context.Background(),
				r.stages[0], makeFrame())

			if outcome != tt.wantOutcome {
				t.Fatalf("outcome: got %d, want %d", outcome, tt.wantOutcome)
			}

			calls := atomic.LoadInt32(&fake.calls)
			if calls != tt.wantCalls {
				t.Fatalf("Transform calls: got %d, want %d", calls, tt.wantCalls)
			}

			if tt.wantDL && dl.len() == 0 {
				t.Fatal("expected dead-letter entry, got none")
			}
			if !tt.wantDL && dl.len() > 0 {
				t.Fatalf("unexpected dead-letter entries: %d", dl.len())
			}
		})
	}
}

func TestCallTransform_DeadLetterReceivesFrame(t *testing.T) {
	t.Parallel()

	dl := &deadLetterCapture{}
	r := newTestRunner()
	r.coord.SetDeadLetter(dl.fn)

	cli := &grpcErrTransform{code: codes.InvalidArgument}
	r.AddTransformer("dl-stage", cli, 50*time.Millisecond, 0, 0, nil)
	r.AddSink(captureFor(r.coord))

	frame := makeFrame()
	outcome, _, _ := r.callTransform(context.Background(), r.stages[0], frame)

	if outcome != outcomeFailed {
		t.Fatalf("outcome: got %d, want outcomeFailed", outcome)
	}
	if dl.len() != 1 {
		t.Fatalf("dead-letter entries: got %d, want 1", dl.len())
	}

	entry := dl.get(0)
	if entry.stage != "dl-stage" {
		t.Fatalf("dead-letter stage: got %q, want %q", entry.stage, "dl-stage")
	}
	if entry.frame != frame {
		t.Fatal("dead-letter must receive the original frame pointer")
	}
	if entry.cause == nil {
		t.Fatal("dead-letter cause must not be nil")
	}
}

func TestCallTransform_ContextCancelled_AbortsImmediately(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		cancelOnFirst bool // true => cancel after the first Transform call returns; false => pre-cancel
		retries       int
		wantCalls     int32
	}{
		{
			name:          "pre_cancelled_context_zero_calls",
			cancelOnFirst: false,
			retries:       10,
			wantCalls:     0,
		},
		{
			name:          "cancelled_during_backoff_stops_quickly",
			cancelOnFirst: true,
			retries:       100,
			wantCalls:     1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			cli := &grpcErrTransform{code: codes.Unavailable}

			if tt.cancelOnFirst {
				// Deterministic gate: cancel only after the fake transform has
				// recorded its first invocation. No wall-clock sleep needed.
				cli.firstCall = make(chan struct{})
				go func() {
					<-cli.firstCall
					cancel()
				}()
			} else {
				cancel()
			}

			r := newTestRunner()
			r.AddTransformer("ctx-test", cli, 50*time.Millisecond, tt.retries, 50*time.Millisecond, nil)
			r.AddSink(captureFor(r.coord))

			start := time.Now()
			outcome, _, _ := r.callTransform(ctx, r.stages[0], makeFrame())
			elapsed := time.Since(start)

			if outcome != outcomeAbort {
				t.Fatalf("outcome: got %d, want outcomeAbort", outcome)
			}

			calls := atomic.LoadInt32(&cli.callCount)
			if calls > tt.wantCalls+1 {
				t.Fatalf("Transform calls: got %d, wanted at most %d", calls, tt.wantCalls+1)
			}

			if elapsed > 1*time.Second {
				t.Fatalf("context cancellation too slow: took %v", elapsed)
			}
		})
	}
}

func TestPushFrame_SingleAckOnDrop(t *testing.T) {
	t.Parallel()

	r := newTestRunner()
	r.AddTransformer("dropper", &fakeTransform{mode: "drop"}, 100*time.Millisecond, 0, 0, nil)
	r.AddSink(&failSink{})

	ac := &ackCounter{}
	r.coord.Subscribe(ac.handler)

	if err := r.pushFrame(context.Background(), makeFrame()); err != nil {
		t.Fatalf("pushFrame: %v", err)
	}

	if got := ac.get(); got != 1 {
		t.Fatalf("ack count: got %d, want exactly 1", got)
	}
}

func TestPushFrame_SingleAckOnPermanentError(t *testing.T) {
	t.Parallel()

	r := newTestRunner()
	r.coord.SetDeadLetter(func(string, *pb.Frame, error) error { return nil })
	r.AddTransformer("broken", &fakeTransform{mode: "permanent_error"}, 100*time.Millisecond, 0, 0, nil)
	r.AddSink(&failSink{})

	ac := &ackCounter{}
	r.coord.Subscribe(ac.handler)

	if err := r.pushFrame(context.Background(), makeFrame()); err != nil {
		t.Fatalf("pushFrame: %v", err)
	}

	if got := ac.get(); got != 1 {
		t.Fatalf("ack count: got %d, want exactly 1", got)
	}
}

func TestPushFrame_SingleAckOnOK(t *testing.T) {
	t.Parallel()

	r := newTestRunner()
	r.AddTransformer("pass", &fakeTransform{mode: "ok"}, 100*time.Millisecond, 0, 0, nil)
	cs := captureFor(r.coord)
	r.AddSink(cs)

	ac := &ackCounter{}
	r.coord.Subscribe(ac.handler)

	if err := r.pushFrame(context.Background(), makeFrame()); err != nil {
		t.Fatalf("pushFrame: %v", err)
	}

	if got := ac.get(); got != 1 {
		t.Fatalf("ack count: got %d, want exactly 1", got)
	}
}

func TestPushFrame_FanOutSingleAck(t *testing.T) {
	t.Parallel()

	r := newTestRunner()
	r.AddTransformer("fan", &fakeTransform{mode: "fanout2"}, 100*time.Millisecond, 0, 0, nil)
	cs := captureFor(r.coord)
	r.AddSink(cs)

	ac := &ackCounter{}
	r.coord.Subscribe(ac.handler)

	if err := r.pushFrame(context.Background(), makeFrame()); err != nil {
		t.Fatalf("pushFrame: %v", err)
	}

	cs.mu.Lock()
	pushed := len(cs.pushed)
	cs.mu.Unlock()
	if pushed != 2 {
		t.Fatalf("pushed frames: got %d, want 2", pushed)
	}

	if got := ac.get(); got != 1 {
		t.Fatalf("ack count: got %d, want exactly 1 (fan-out must not multiply acks)", got)
	}
}

func TestRunner_SourceErr(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		source   *fakeSource
		wantErr  bool
		wantKind qerr.Kind
	}{
		{
			name:     "source_fails_propagates_pipeline_error",
			source:   &fakeSource{runErr: errors.New("broker down")},
			wantErr:  true,
			wantKind: qerr.KindPipeline,
		},
		{
			name:    "source_blocks_then_cancelled_no_error",
			source:  &fakeSource{block: true},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := newTestRunner()
			r.SetSource(tt.source)
			r.AddSink(captureFor(r.coord))

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			if err := r.Start(ctx); err != nil {
				t.Fatalf("Start: %v", err)
			}

			if tt.wantErr {
				select {
				case err := <-r.SourceErr():
					if err == nil {
						t.Fatal("expected non-nil error on SourceErr channel")
					}
					if !qerr.IsKind(err, tt.wantKind) {
						t.Fatalf("expected kind %v, got: %v", tt.wantKind, err)
					}
				case <-time.After(2 * time.Second):
					t.Fatal("timed out waiting for SourceErr")
				}
			} else {
				cancel()
				select {
				case err := <-r.SourceErr():
					t.Fatalf("unexpected error on SourceErr: %v", err)
				case <-time.After(200 * time.Millisecond):
				}
			}
		})
	}
}

func TestRunner_Start_NoSource(t *testing.T) {
	t.Parallel()

	r := newTestRunner()
	err := r.Start(context.Background())
	if err == nil {
		t.Fatal("expected error when no source configured")
	}
	if !qerr.IsPipeline(err) {
		t.Fatalf("expected pipeline error, got: %v", err)
	}
}

func TestRunner_Close_ErrorAggregation(t *testing.T) {
	t.Parallel()

	errTransform := errors.New("transform close failed")
	errSink := errors.New("sink close failed")

	tests := []struct {
		name          string
		transformErr  error
		sinkErr       error
		wantNil       bool
		wantTransform bool
		wantSink      bool
	}{
		{name: "no_errors_returns_nil", wantNil: true},
		{name: "transform_error", transformErr: errTransform, wantTransform: true},
		{name: "sink_error", sinkErr: errSink, wantSink: true},
		{name: "both_joined", transformErr: errTransform, sinkErr: errSink, wantTransform: true, wantSink: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := newTestRunner()
			if tt.transformErr != nil {
				r.AddTransformer("broken", &failTransformClose{err: tt.transformErr}, 0, 0, 0, nil)
			} else {
				r.AddTransformer("ok", &fakeTransform{mode: "ok"}, 0, 0, 0, nil)
			}
			if tt.sinkErr != nil {
				r.AddSink(&failSink{err: tt.sinkErr})
			} else {
				r.AddSink(captureFor(r.coord))
			}

			err := r.Close(context.Background())
			if tt.wantNil {
				if err != nil {
					t.Fatalf("expected nil, got: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if tt.wantTransform && !errors.Is(err, tt.transformErr) {
				t.Fatalf("missing transform error in chain: %v", err)
			}
			if tt.wantSink && !errors.Is(err, tt.sinkErr) {
				t.Fatalf("missing sink error in chain: %v", err)
			}
		})
	}
}

func TestRunner_BackoffOrCancel(t *testing.T) {
	t.Parallel()

	// backoffOrCancel either returns true after duration elapses, or false
	// immediately when ctx is done. Tests cover both branches deterministically:
	//   - completes_normally:    short duration, ctx never cancelled
	//   - already_cancelled:     ctx cancelled before the call (no sleeps)
	tests := []struct {
		name       string
		duration   time.Duration
		preCancel  bool
		wantResult bool
	}{
		{name: "completes_normally", duration: 5 * time.Millisecond, wantResult: true},
		{name: "already_cancelled", duration: 5 * time.Second, preCancel: true, wantResult: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := newTestRunner()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tt.preCancel {
				cancel()
			}

			got := r.backoffOrCancel(ctx, tt.duration)
			if got != tt.wantResult {
				t.Fatalf("got %v, want %v", got, tt.wantResult)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Phase 2 — NackAware binding + DLQ wiring in Runner
// ---------------------------------------------------------------------------

// nackCaptureSink advertises both ack and nack capabilities.
type nackCaptureSink struct {
	mu         sync.Mutex
	pushed     []*pb.Frame
	ackFn      sink.EmitFn
	nackFn     sink.NackFn
	publishErr error
}

func (s *nackCaptureSink) Name() string { return "nack-capture" }
func (s *nackCaptureSink) Caps() sink.Capabilities {
	return sink.Capabilities{AckAware: true, NackAware: true}
}

func (s *nackCaptureSink) Publish(_ context.Context, f *pb.Frame) error {
	s.mu.Lock()
	s.pushed = append(s.pushed, f)
	s.mu.Unlock()
	if s.publishErr != nil {
		return s.publishErr
	}
	if s.ackFn != nil {
		s.ackFn(context.Background(), f.Checkpoint)
	}
	return nil
}
func (s *nackCaptureSink) Close(context.Context) error { return nil }

var _ sink.Adapter = (*nackCaptureSink)(nil)

// nackCaptureFor returns a fixture wired to the runner's coordinator.
func nackCaptureFor(coord *AckCoordinator) *nackCaptureSink {
	return &nackCaptureSink{ackFn: coord.Ack, nackFn: coord.Nack}
}

func TestAddSink_CountsAckAware(t *testing.T) {
	t.Parallel()

	r := newTestRunner()
	ns := nackCaptureFor(r.coord)
	r.AddSink(ns)

	if r.ackAwareSinks != 1 {
		t.Fatalf("ackAwareSinks: got %d, want 1", r.ackAwareSinks)
	}
}

func TestRunner_SetDLQSink(t *testing.T) {
	t.Parallel()

	r := newTestRunner()
	dlq := &fakeDLQAdapter{}
	r.SetDLQSink(dlq)

	if !r.coord.HasDLQ() {
		t.Fatal("DLQ must be configured on coordinator after SetDLQSink")
	}
}

func TestRunner_CloseDLQSink(t *testing.T) {
	t.Parallel()

	r := newTestRunner()
	dlq := &fakeDLQAdapter{}
	r.SetDLQSink(dlq)

	if err := r.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !dlq.closed {
		t.Fatal("DLQ sink must be closed by Runner.Close")
	}
}

func TestPublishAll_SyncSinkFail_NacksWithDLQ(t *testing.T) {
	t.Parallel()

	r := newTestRunner()
	r.AddTransformer("pass", &fakeTransform{mode: "ok"}, 100*time.Millisecond, 0, 0, nil)

	// Sync sink that fails every publish.
	errSink := &publishErrSink{err: errors.New("broker down")}
	r.AddSink(errSink)

	dlq := &fakeDLQAdapter{}
	r.SetDLQSink(dlq)

	ac := &ackCounter{}
	r.coord.Subscribe(ac.handler)

	err := r.pushFrame(context.Background(), makeFrame())
	// With DLQ configured, sync publish failure should nack → DLQ → commit.
	// pushFrame returns nil (frame handled via DLQ path).
	if err != nil {
		t.Fatalf("pushFrame with DLQ should not return error: %v", err)
	}

	if dlq.published == nil {
		t.Fatal("DLQ sink must receive the failed frame")
	}
	if ac.get() != 1 {
		t.Fatalf("commit count: got %d, want 1 (nack → DLQ → commit)", ac.get())
	}
}

func TestPublishAll_SyncSinkFail_NoDLQ_ReturnsError(t *testing.T) {
	t.Parallel()

	r := newTestRunner()
	r.AddTransformer("pass", &fakeTransform{mode: "ok"}, 100*time.Millisecond, 0, 0, nil)

	errSink := &publishErrSink{err: errors.New("broker down")}
	r.AddSink(errSink)
	// No DLQ configured

	ac := &ackCounter{}
	r.coord.Subscribe(ac.handler)

	err := r.pushFrame(context.Background(), makeFrame())
	// Without DLQ, sync publish failure returns error (current behavior).
	if err == nil {
		t.Fatal("pushFrame without DLQ should return error on sync sink failure")
	}
	if ac.get() != 0 {
		t.Fatalf("commit count: got %d, want 0 (no DLQ, barrier aborted)", ac.get())
	}
}

// publishErrSink is a sync sink that fails Publish with a given error.
type publishErrSink struct {
	err error
}

func (s *publishErrSink) Name() string                                 { return "publish-err" }
func (s *publishErrSink) Caps() sink.Capabilities                      { return sink.Capabilities{} }
func (s *publishErrSink) Publish(_ context.Context, _ *pb.Frame) error { return s.err }
func (s *publishErrSink) Close(context.Context) error                  { return nil }

// fakeDLQAdapter satisfies both DLQPublisher (for coordinator) and sink.Adapter
// (for Runner lifecycle). Captures published frames for assertion.
type fakeDLQAdapter struct {
	published *pb.Frame
	closed    bool
}

func (d *fakeDLQAdapter) Name() string            { return "fake-dlq" }
func (d *fakeDLQAdapter) Caps() sink.Capabilities { return sink.Capabilities{} }
func (d *fakeDLQAdapter) Publish(_ context.Context, f *pb.Frame) error {
	d.published = f
	return nil
}

func (d *fakeDLQAdapter) Close(context.Context) error {
	d.closed = true
	return nil
}

// errorEventsTransform returns error_events in the response alongside normal events.
type errorEventsTransform struct {
	fakeTransform
}

func (e *errorEventsTransform) Transform(_ context.Context, req *pb.TransformRequest) (*pb.TransformResponse, error) {
	return &pb.TransformResponse{
		Status:      pb.Status_OK,
		Events:      []*pb.Event{{Value: []byte("good")}},
		ErrorEvents: []*pb.Event{{Value: []byte("bad"), Metadata: &pb.EventMetadata{Headers: map[string]string{"dlq-error": "rejected"}}}},
	}, nil
}

func TestRunStage_ErrorEventsRoutedToErrorSink(t *testing.T) {
	t.Parallel()

	r := newTestRunner()
	errCapture := captureFor(r.coord)
	r.AddTransformer("ce-norm", &errorEventsTransform{}, 100*time.Millisecond, 0, 0, errCapture)

	outSink := captureFor(r.coord)
	r.AddSink(outSink)

	if err := r.pushFrame(context.Background(), makeFrame()); err != nil {
		t.Fatalf("pushFrame: %v", err)
	}

	outSink.mu.Lock()
	gotOut := len(outSink.pushed)
	outSink.mu.Unlock()
	if gotOut != 1 {
		t.Fatalf("output frames: got %d, want 1", gotOut)
	}
	if string(outSink.pushed[0].Value) != "good" {
		t.Fatalf("output value: got %q, want %q", string(outSink.pushed[0].Value), "good")
	}

	errCapture.mu.Lock()
	gotErr := len(errCapture.pushed)
	errCapture.mu.Unlock()
	if gotErr != 1 {
		t.Fatalf("error_events frames: got %d, want 1", gotErr)
	}
	if string(errCapture.pushed[0].Value) != "bad" {
		t.Fatalf("error value: got %q, want %q", string(errCapture.pushed[0].Value), "bad")
	}
	if string(errCapture.pushed[0].Headers["dlq-error"]) != "rejected" {
		t.Fatalf("error header: got %q, want %q", string(errCapture.pushed[0].Headers["dlq-error"]), "rejected")
	}
}

func TestRunStage_ErrorEventsNoSink_WarnsAndDrops(t *testing.T) {
	t.Parallel()

	r := newTestRunner()
	// No error sink configured (nil)
	r.AddTransformer("ce-norm", &errorEventsTransform{}, 100*time.Millisecond, 0, 0, nil)

	outSink := captureFor(r.coord)
	r.AddSink(outSink)

	// Should not panic or error — just warn and drop error_events
	if err := r.pushFrame(context.Background(), makeFrame()); err != nil {
		t.Fatalf("pushFrame: %v", err)
	}

	outSink.mu.Lock()
	gotOut := len(outSink.pushed)
	outSink.mu.Unlock()
	if gotOut != 1 {
		t.Fatalf("output frames: got %d, want 1 (normal events should still flow)", gotOut)
	}
}

// errorEventsOnlyTransform returns only error_events, no normal events.
type errorEventsOnlyTransform struct {
	fakeTransform
}

func (e *errorEventsOnlyTransform) Transform(_ context.Context, req *pb.TransformRequest) (*pb.TransformResponse, error) {
	return &pb.TransformResponse{
		Status:      pb.Status_OK,
		ErrorEvents: []*pb.Event{{Value: []byte("rejected-only")}},
	}, nil
}

func TestRunStage_ErrorEventsOnly_NoOutputFrames(t *testing.T) {
	t.Parallel()

	r := newTestRunner()
	errCapture := captureFor(r.coord)
	r.AddTransformer("ce-norm", &errorEventsOnlyTransform{}, 100*time.Millisecond, 0, 0, errCapture)

	outSink := captureFor(r.coord)
	r.AddSink(outSink)

	if err := r.pushFrame(context.Background(), makeFrame()); err != nil {
		t.Fatalf("pushFrame: %v", err)
	}

	outSink.mu.Lock()
	gotOut := len(outSink.pushed)
	outSink.mu.Unlock()
	if gotOut != 0 {
		t.Fatalf("output frames: got %d, want 0 (all rejected)", gotOut)
	}

	errCapture.mu.Lock()
	gotErr := len(errCapture.pushed)
	errCapture.mu.Unlock()
	if gotErr != 1 {
		t.Fatalf("error_events frames: got %d, want 1", gotErr)
	}
	if string(errCapture.pushed[0].Value) != "rejected-only" {
		t.Fatalf("error value: got %q, want %q", string(errCapture.pushed[0].Value), "rejected-only")
	}
}

func TestRunner_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		setup   func(r *Runner)
		wantErr bool
		wantMsg string
	}{
		{
			name:    "no_source",
			setup:   func(r *Runner) { r.AddSink(captureFor(r.coord)) },
			wantErr: true,
			wantMsg: "no source configured",
		},
		{
			name:    "no_sinks",
			setup:   func(r *Runner) { r.SetSource(&fakeSource{block: true}) },
			wantErr: true,
			wantMsg: "no sinks configured",
		},
		{
			name: "ok",
			setup: func(r *Runner) {
				r.SetSource(&fakeSource{block: true})
				r.AddSink(captureFor(r.coord))
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := newTestRunner()
			tt.setup(r)

			err := r.Validate()
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !qerr.IsPipeline(err) {
					t.Fatalf("want pipeline kind, got: %v", err)
				}
				if !strings.Contains(err.Error(), tt.wantMsg) {
					t.Fatalf("want message containing %q, got: %v", tt.wantMsg, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Validate: unexpected error: %v", err)
			}
		})
	}
}

func TestRunner_Start_RejectsSecondCall(t *testing.T) {
	t.Parallel()

	r := newTestRunner()
	r.SetSource(&fakeSource{block: true})
	r.AddSink(captureFor(r.coord))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := r.Start(ctx); err != nil {
		t.Fatalf("first Start: unexpected error: %v", err)
	}

	err := r.Start(ctx)
	if err == nil {
		t.Fatal("second Start must return an error")
	}
	if !qerr.IsPipeline(err) {
		t.Fatalf("want pipeline kind, got: %v", err)
	}
	if !strings.Contains(err.Error(), "already started") {
		t.Fatalf("want message containing 'already started', got: %v", err)
	}
}

func TestRunner_Start_ConcurrentCallsExactlyOneWins(t *testing.T) {
	t.Parallel()

	r := newTestRunner()
	r.SetSource(&fakeSource{block: true})
	r.AddSink(captureFor(r.coord))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	const goroutines = 16
	var wg sync.WaitGroup
	wg.Add(goroutines)

	var successes atomic.Int32
	var failures atomic.Int32
	start := make(chan struct{})

	for range goroutines {
		go func() {
			defer wg.Done()
			<-start
			if err := r.Start(ctx); err == nil {
				successes.Add(1)
			} else {
				failures.Add(1)
			}
		}()
	}

	close(start)
	wg.Wait()

	if successes.Load() != 1 {
		t.Fatalf("want exactly 1 successful Start, got %d", successes.Load())
	}
	if failures.Load() != goroutines-1 {
		t.Fatalf("want %d failures, got %d", goroutines-1, failures.Load())
	}
}
