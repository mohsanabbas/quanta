package pipeline

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	pb "quanta/api/proto/v1"
)

func TestNack_AbortAndDLQ(t *testing.T) {
	t.Parallel()

	ac := &ackCounter{}
	coord := NewAckCoordinator()
	coord.Subscribe(ac.handler)
	coord.SetDLQSink(&fakeDLQSink{})

	tok := kafkaTok("t", 0, 100)
	coord.Barrier(tok, 1)

	frame := &pb.Frame{
		Key:        []byte("k"),
		Value:      []byte("bad-payload"),
		Checkpoint: tok,
		Headers:    map[string][]byte{"source": []byte("orders")},
	}

	coord.Nack(context.Background(), frame, errTest)

	if coord.Len() != 0 {
		t.Fatalf("barrier should be removed after nack: Len=%d", coord.Len())
	}

	if got := ac.get(); got != 1 {
		t.Fatalf("commit count after nack+DLQ: got %d, want 1", got)
	}
}

func TestNack_NoDLQ_Withholds(t *testing.T) {
	t.Parallel()

	ac := &ackCounter{}
	coord := NewAckCoordinator()
	coord.Subscribe(ac.handler)

	tok := kafkaTok("t", 0, 200)
	coord.Barrier(tok, 1)

	frame := &pb.Frame{Value: []byte("bad"), Checkpoint: tok}
	coord.Nack(context.Background(), frame, errTest)

	if coord.Len() != 0 {
		t.Fatalf("barrier should be removed after nack: Len=%d", coord.Len())
	}

	if got := ac.get(); got != 0 {
		t.Fatalf("commit count without DLQ: got %d, want 0 (withhold for redelivery)", got)
	}
}

func TestNack_DLQFails_Withholds(t *testing.T) {
	t.Parallel()

	ac := &ackCounter{}
	coord := NewAckCoordinator()
	coord.Subscribe(ac.handler)
	coord.SetDLQSink(&fakeDLQSink{err: errTest})

	tok := kafkaTok("t", 0, 300)
	coord.Barrier(tok, 1)

	frame := &pb.Frame{Value: []byte("bad"), Checkpoint: tok}
	coord.Nack(context.Background(), frame, errTest)

	if coord.Len() != 0 {
		t.Fatalf("barrier should be removed after nack: Len=%d", coord.Len())
	}

	if got := ac.get(); got != 0 {
		t.Fatalf("commit count on DLQ failure: got %d, want 0 (withhold for redelivery)", got)
	}
}

func TestNack_NilFrame_Safe(t *testing.T) {
	t.Parallel()

	coord := NewAckCoordinator()
	coord.SetDLQSink(&fakeDLQSink{})

	coord.Nack(context.Background(), nil, errTest)
}

func TestNack_NilCheckpoint_Safe(t *testing.T) {
	t.Parallel()

	ac := &ackCounter{}
	coord := NewAckCoordinator()
	coord.Subscribe(ac.handler)
	coord.SetDLQSink(&fakeDLQSink{})

	frame := &pb.Frame{Value: []byte("no-checkpoint")}
	coord.Nack(context.Background(), frame, errTest)

	if got := ac.get(); got != 0 {
		t.Fatalf("commit count on nil checkpoint nack: got %d, want 0", got)
	}
}

func TestHasDLQ(t *testing.T) {
	t.Parallel()

	coord := NewAckCoordinator()
	if coord.HasDLQ() {
		t.Fatal("HasDLQ should be false when no DLQ configured")
	}

	coord.SetDLQSink(&fakeDLQSink{})
	if !coord.HasDLQ() {
		t.Fatal("HasDLQ should be true after SetDLQSink")
	}
}

func TestSetDLQSink_Replace(t *testing.T) {
	t.Parallel()

	ac := &ackCounter{}
	coord := NewAckCoordinator()
	coord.Subscribe(ac.handler)

	failSink := &fakeDLQSink{err: errTest}
	okSink := &fakeDLQSink{}

	coord.SetDLQSink(failSink)
	coord.SetDLQSink(okSink)

	tok := kafkaTok("t", 0, 400)
	coord.Barrier(tok, 1)
	coord.Nack(context.Background(), &pb.Frame{Value: []byte("v"), Checkpoint: tok}, errTest)

	if got := ac.get(); got != 1 {
		t.Fatalf("commit count: got %d, want 1 (replaced sink should be used)", got)
	}
}

func TestNack_BuildDLQFrame(t *testing.T) {
	t.Parallel()

	dlqSink := &captureDLQSink{}
	coord := NewAckCoordinator()
	coord.SetDLQSink(dlqSink)

	tok := kafkaTok("t", 0, 500)
	coord.Barrier(tok, 1)

	original := &pb.Frame{
		Key:   []byte("order-123"),
		Value: []byte(`{"broken": true}`),
		Headers: map[string][]byte{
			"content-type": []byte("application/json"),
		},
		Checkpoint: tok,
	}

	coord.Nack(context.Background(), original, errTest)

	if dlqSink.published == nil {
		t.Fatal("DLQ sink did not receive a frame")
	}

	dlqFrame := dlqSink.published

	if string(dlqFrame.Value) != `{"broken": true}` {
		t.Fatalf("DLQ frame value: got %q", dlqFrame.Value)
	}

	if errMsg := dlqFrame.Headers["x-dlq-error"]; string(errMsg) != "test error" {
		t.Fatalf("DLQ error header: got %q, want %q", errMsg, "test error")
	}

	if ct := dlqFrame.Headers["content-type"]; string(ct) != "application/json" {
		t.Fatalf("DLQ original header: got %q", ct)
	}

	if dlqFrame.Checkpoint == nil {
		t.Fatal("DLQ frame must carry the checkpoint token")
	}
}

func TestNack_ConcurrentSafe(t *testing.T) {
	t.Parallel()

	var commitCount atomic.Int32
	coord := NewAckCoordinator()
	coord.Subscribe(func(*pb.ConnectorAck) {
		commitCount.Add(1)
	})
	coord.SetDLQSink(&fakeDLQSink{})

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := range goroutines {
		go func(offset int64) {
			defer wg.Done()
			tok := kafkaTok("t", 0, offset)
			coord.Barrier(tok, 1)
			coord.Nack(context.Background(), &pb.Frame{Value: []byte("v"), Checkpoint: tok}, errTest)
		}(int64(i))
	}
	wg.Wait()

	if got := commitCount.Load(); got != int32(goroutines) {
		t.Fatalf("concurrent nack commits: got %d, want %d", got, goroutines)
	}
	if coord.Len() != 0 {
		t.Fatalf("all barriers should be cleaned up: Len=%d", coord.Len())
	}
}

func TestNack_BuildDLQFrame_HeaderByteIsolation(t *testing.T) {
	t.Parallel()

	dlqSink := &captureDLQSink{}
	coord := NewAckCoordinator()
	coord.SetDLQSink(dlqSink)

	tok := kafkaTok("t", 0, 600)
	coord.Barrier(tok, 1)

	original := &pb.Frame{
		Key:   []byte("k"),
		Value: []byte("v"),
		Headers: map[string][]byte{
			"trace-id": []byte("abc-123"),
		},
		Checkpoint: tok,
	}

	coord.Nack(context.Background(), original, errTest)

	dlqFrame := dlqSink.published
	copy(dlqFrame.Headers["trace-id"], "XXXXXX")

	if string(original.Headers["trace-id"]) != "abc-123" {
		t.Fatalf("original header corrupted: got %q, want %q",
			original.Headers["trace-id"], "abc-123")
	}
}

func TestNack_AckRace_NackWins(t *testing.T) {
	t.Parallel()

	ac := &ackCounter{}
	coord := NewAckCoordinator()
	coord.Subscribe(ac.handler)
	coord.SetDLQSink(&fakeDLQSink{})

	tok := kafkaTok("t", 0, 700)
	coord.Barrier(tok, 2)

	frame := &pb.Frame{Value: []byte("v"), Checkpoint: tok}

	coord.Nack(context.Background(), frame, errTest)

	coord.Ack(context.Background(), tok)

	if got := ac.get(); got != 1 {
		t.Fatalf("commit count: got %d, want 1 (nack DLQ path only)", got)
	}
	if coord.Len() != 0 {
		t.Fatalf("barrier should be cleaned up: Len=%d", coord.Len())
	}
}

func TestDLQPublisher_SinkAdapterSatisfies(t *testing.T) {
	t.Parallel()

	var pub DLQPublisher = &fakeDLQSink{}
	if err := pub.Publish(context.Background(), &pb.Frame{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

type fakeDLQSink struct {
	err error
}

func (s *fakeDLQSink) Publish(_ context.Context, _ *pb.Frame) error { return s.err }

type captureDLQSink struct {
	published *pb.Frame
}

func (s *captureDLQSink) Publish(_ context.Context, f *pb.Frame) error { s.published = f; return nil }
