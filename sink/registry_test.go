package sink

import (
	"context"
	"testing"

	pb "quanta/api/proto/v1"
)

func TestNackFn_Signature(t *testing.T) {
	t.Parallel()

	var called bool
	var fn NackFn = func(_ context.Context, frame *pb.Frame, err error) {
		called = true
	}
	fn(context.Background(), &pb.Frame{Value: []byte("v")}, errStub)
	if !called {
		t.Fatal("NackFn was not invoked")
	}
}

func TestNackAware_BindNack(t *testing.T) {
	t.Parallel()

	s := &nackCaptureSink{}
	var iface NackAware = s

	var captured *pb.Frame
	iface.BindNack(func(_ context.Context, frame *pb.Frame, err error) {
		captured = frame
	})

	f := &pb.Frame{Key: []byte("k"), Value: []byte("v")}
	s.simulateFailure(context.Background(), f, errStub)

	if captured == nil {
		t.Fatal("NackFn was not called on simulated failure")
	}
	if string(captured.Key) != "k" {
		t.Fatalf("captured frame key: got %q, want %q", captured.Key, "k")
	}
}

func TestNackAware_AckAware_Coexistence(t *testing.T) {
	t.Parallel()

	s := &dualAwareSink{}
	var _ AckAware = s
	var _ NackAware = s
}

var errStub = errorString("stub error")

type errorString string

func (e errorString) Error() string { return string(e) }

type nackCaptureSink struct {
	nackFn NackFn
}

func (s *nackCaptureSink) BindNack(fn NackFn) {
	s.nackFn = fn
}

func (s *nackCaptureSink) simulateFailure(ctx context.Context, f *pb.Frame, err error) {
	if s.nackFn != nil {
		s.nackFn(ctx, f, err)
	}
}

type dualAwareSink struct {
	ackFn  EmitFn
	nackFn NackFn
}

func (s *dualAwareSink) BindAck(fn EmitFn)  { s.ackFn = fn }
func (s *dualAwareSink) BindNack(fn NackFn) { s.nackFn = fn }
