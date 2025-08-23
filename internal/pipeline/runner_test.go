package pipeline

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	pb "quanta/api/proto/v1"
	"quanta/sink"

	"google.golang.org/grpc"
)

type fakeTransform struct {
	calls int32
	mode  string
}

func (f *fakeTransform) Metadata(ctx context.Context) (*pb.MetadataResponse, error) {
	return &pb.MetadataResponse{}, nil
}
func (f *fakeTransform) Health(ctx context.Context) (*pb.HealthResponse, error) {
	return &pb.HealthResponse{Ok: true}, nil
}
func (f *fakeTransform) Stream(ctx context.Context, opts ...grpc.CallOption) (pb.TransformService_TransformStreamClient, error) {
	return nil, nil
}
func (f *fakeTransform) Close() error { return nil }
func (f *fakeTransform) Transform(ctx context.Context, req *pb.TransformRequest) (*pb.TransformResponse, error) {
	c := atomic.AddInt32(&f.calls, 1)
	switch f.mode {
	case "ok":
		return &pb.TransformResponse{Status: pb.Status_OK, Events: []*pb.Event{{Value: append([]byte{}, req.Payload...)}}}, nil
	case "drop":
		return &pb.TransformResponse{Status: pb.Status_DROP}, nil
	case "errorThenOK":
		if c == 1 {
			return &pb.TransformResponse{Status: pb.Status_ERROR}, nil
		}
		return &pb.TransformResponse{Status: pb.Status_OK, Events: []*pb.Event{{Value: append([]byte{}, req.Payload...)}}}, nil
	case "fanout2":
		return &pb.TransformResponse{Status: pb.Status_OK, Events: []*pb.Event{{Value: append([]byte{}, req.Payload...)}, {Value: append([]byte{}, req.Payload...)}}}, nil
	default:
		return &pb.TransformResponse{Status: pb.Status_OK, Events: []*pb.Event{{Value: append([]byte{}, req.Payload...)}}}, nil
	}
}

type captureSink struct {
	pushed []*pb.Frame
	ackFn  sink.EmitFn
}

func (c *captureSink) Configure(any) error { return nil }
func (c *captureSink) Push(f *pb.Frame) error {
	c.pushed = append(c.pushed, f)
	if c.ackFn != nil {
		c.ackFn(f.Checkpoint)
	}
	return nil
}
func (c *captureSink) Close() error           { return nil }
func (c *captureSink) BindAck(fn sink.EmitFn) { c.ackFn = fn }

func makeFrame() *pb.Frame {
	return &pb.Frame{Value: []byte("hello"), Checkpoint: &pb.CheckpointToken{Kind: &pb.CheckpointToken_Kafka{Kafka: &pb.KafkaOffset{Topic: "t", Partition: 1, Offset: 42}}}}
}

func TestRunner_TransformerOK_ForwardsAndSinkAcks(t *testing.T) {
	r := NewRunner()
	fake := &fakeTransform{mode: "ok"}
	r.AddTransformer("t1", fake, 100*time.Millisecond, 0, 0)
	cs := &captureSink{}
	cs.BindAck(r.Ack)
	r.AddSink(cs)

	f := makeFrame()
	if err := r.pushFrame(f); err != nil {
		t.Fatalf("pushFrame: %v", err)
	}
	if len(cs.pushed) != 1 {
		t.Fatalf("expected 1 pushed frame, got %d", len(cs.pushed))
	}
	if string(cs.pushed[0].Value) != "hello" {
		t.Fatalf("unexpected value: %q", cs.pushed[0].Value)
	}
}

func TestRunner_TransformerDrop_AcksNoPush(t *testing.T) {
	r := NewRunner()
	fake := &fakeTransform{mode: "drop"}
	r.AddTransformer("t1", fake, 100*time.Millisecond, 0, 0)
	cs := &captureSink{}
	cs.BindAck(r.Ack)
	r.AddSink(cs)

	f := makeFrame()
	if err := r.pushFrame(f); err != nil {
		t.Fatalf("pushFrame: %v", err)
	}
	if len(cs.pushed) != 0 {
		t.Fatalf("expected 0 pushed frames on DROP, got %d", len(cs.pushed))
	}
}

func TestRunner_TransformerRetryThenOK(t *testing.T) {
	r := NewRunner()
	fake := &fakeTransform{mode: "errorThenOK"}

	r.AddTransformer("t1", fake, 100*time.Millisecond, 1, 1*time.Millisecond)
	cs := &captureSink{}
	cs.BindAck(r.Ack)
	r.AddSink(cs)

	f := makeFrame()
	if err := r.pushFrame(f); err != nil {
		t.Fatalf("pushFrame: %v", err)
	}
	if len(cs.pushed) != 1 {
		t.Fatalf("expected 1 pushed frame after retry, got %d", len(cs.pushed))
	}
}

func TestRunner_MultiStageFanout(t *testing.T) {
	r := NewRunner()

	stage1 := &fakeTransform{mode: "fanout2"}
	stage2 := &fakeTransform{mode: "ok"}
	r.AddTransformer("s1", stage1, 100*time.Millisecond, 0, 0)
	r.AddTransformer("s2", stage2, 100*time.Millisecond, 0, 0)
	cs := &captureSink{}
	cs.BindAck(r.Ack)
	r.AddSink(cs)

	f := makeFrame()
	if err := r.pushFrame(f); err != nil {
		t.Fatalf("pushFrame: %v", err)
	}
	if len(cs.pushed) != 2 {
		t.Fatalf("expected 2 pushed frames after fanout, got %d", len(cs.pushed))
	}
}
