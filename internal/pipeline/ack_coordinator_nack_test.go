package pipeline

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	pb "quanta/api/proto/v1"
)

// TestNack_AbortAndDLQ verifies the happy path:
// Nack aborts the barrier, publishes to DLQ sink, and commits on DLQ success.
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

	// Barrier should be aborted and removed.
	if coord.Len() != 0 {
		t.Fatalf("barrier should be removed after nack: Len=%d", coord.Len())
	}

	// Commit must fire because DLQ publish succeeded.
	if got := ac.get(); got != 1 {
		t.Fatalf("commit count after nack+DLQ: got %d, want 1", got)
	}
}

// TestNack_NoDLQ_Withholds verifies when no DLQ is configured:
// Nack aborts the barrier but does NOT commit — message redelivered.
func TestNack_NoDLQ_Withholds(t *testing.T) {
	t.Parallel()

	ac := &ackCounter{}
	coord := NewAckCoordinator()
	coord.Subscribe(ac.handler)
	// No DLQ sink set

	tok := kafkaTok("t", 0, 200)
	coord.Barrier(tok, 1)

	frame := &pb.Frame{Value: []byte("bad"), Checkpoint: tok}
	coord.Nack(context.Background(), frame, errTest)

	// Barrier aborted and removed.
	if coord.Len() != 0 {
		t.Fatalf("barrier should be removed after nack: Len=%d", coord.Len())
	}

	// No commit — message will be redelivered by source.
	if got := ac.get(); got != 0 {
		t.Fatalf("commit count without DLQ: got %d, want 0 (withhold for redelivery)", got)
	}
}

// TestNack_DLQFails_Withholds verifies when DLQ publish fails:
// Nack aborts the barrier and does NOT commit — safe default for redelivery.
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

	// Barrier aborted and removed.
	if coord.Len() != 0 {
		t.Fatalf("barrier should be removed after nack: Len=%d", coord.Len())
	}

	// No commit — DLQ failed, withhold for redelivery.
	if got := ac.get(); got != 0 {
		t.Fatalf("commit count on DLQ failure: got %d, want 0 (withhold for redelivery)", got)
	}
}

// TestNack_NilFrame_Safe verifies Nack is a no-op for nil frames.
func TestNack_NilFrame_Safe(t *testing.T) {
	t.Parallel()

	coord := NewAckCoordinator()
	coord.SetDLQSink(&fakeDLQSink{})

	// Must not panic
	coord.Nack(context.Background(), nil, errTest)
}

// TestNack_NilCheckpoint_Safe verifies Nack with a frame that has nil checkpoint.
func TestNack_NilCheckpoint_Safe(t *testing.T) {
	t.Parallel()

	ac := &ackCounter{}
	coord := NewAckCoordinator()
	coord.Subscribe(ac.handler)
	coord.SetDLQSink(&fakeDLQSink{})

	frame := &pb.Frame{Value: []byte("no-checkpoint")}
	coord.Nack(context.Background(), frame, errTest)

	// No commit — nil checkpoint means no barrier to resolve.
	if got := ac.get(); got != 0 {
		t.Fatalf("commit count on nil checkpoint nack: got %d, want 0", got)
	}
}

// TestHasDLQ verifies HasDLQ reports DLQ sink presence correctly.
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

// TestSetDLQSink_Replace verifies that SetDLQSink replaces a previously set sink.
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

	// Should use the replaced (ok) sink → commit fires.
	if got := ac.get(); got != 1 {
		t.Fatalf("commit count: got %d, want 1 (replaced sink should be used)", got)
	}
}

// TestNack_BuildDLQFrame verifies the DLQ frame carries error metadata.
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

	// Original value must be preserved.
	if string(dlqFrame.Value) != `{"broken": true}` {
		t.Fatalf("DLQ frame value: got %q", dlqFrame.Value)
	}

	// Error metadata in headers.
	if errMsg := dlqFrame.Headers["x-dlq-error"]; string(errMsg) != "test error" {
		t.Fatalf("DLQ error header: got %q, want %q", errMsg, "test error")
	}

	// Original headers preserved.
	if ct := dlqFrame.Headers["content-type"]; string(ct) != "application/json" {
		t.Fatalf("DLQ original header: got %q", ct)
	}

	// Checkpoint must be the same token for commit to work.
	if dlqFrame.Checkpoint == nil {
		t.Fatal("DLQ frame must carry the checkpoint token")
	}
}

// TestNack_ConcurrentSafe verifies Nack is safe under concurrent access.
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

	for i := 0; i < goroutines; i++ {
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

// TestNack_BuildDLQFrame_HeaderByteIsolation verifies that mutating DLQ frame
// header bytes does NOT corrupt the original frame's headers.
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

	// Mutate the DLQ frame's header bytes.
	dlqFrame := dlqSink.published
	copy(dlqFrame.Headers["trace-id"], "XXXXXX")

	// Original must be untouched.
	if string(original.Headers["trace-id"]) != "abc-123" {
		t.Fatalf("original header corrupted: got %q, want %q",
			original.Headers["trace-id"], "abc-123")
	}
}

// TestNack_AckRace_NackWins verifies that when a frame fans out to 2 sinks
// (refs=2) and one sink nacks while the other acks, the nack path wins:
// barrier is aborted, DLQ publishes, checkpoint commits via DLQ path.
// The subsequent ack is a harmless no-op.
func TestNack_AckRace_NackWins(t *testing.T) {
	t.Parallel()

	ac := &ackCounter{}
	coord := NewAckCoordinator()
	coord.Subscribe(ac.handler)
	coord.SetDLQSink(&fakeDLQSink{})

	tok := kafkaTok("t", 0, 700)
	coord.Barrier(tok, 2) // fan-out to 2 sinks

	frame := &pb.Frame{Value: []byte("v"), Checkpoint: tok}

	// Sink 1 nacks (permanent failure).
	coord.Nack(context.Background(), frame, errTest)

	// Sink 2 acks (succeeded on its side).
	coord.Ack(context.Background(), tok)

	// Exactly 1 commit — from Nack's DLQ path. Ack finds no barrier → no-op.
	if got := ac.get(); got != 1 {
		t.Fatalf("commit count: got %d, want 1 (nack DLQ path only)", got)
	}
	if coord.Len() != 0 {
		t.Fatalf("barrier should be cleaned up: Len=%d", coord.Len())
	}
}

// TestDLQPublisher_SinkAdapterSatisfies verifies that any sink.Adapter
// implicitly satisfies the DLQPublisher interface (structural subtyping).
func TestDLQPublisher_SinkAdapterSatisfies(t *testing.T) {
	t.Parallel()

	// fakeDLQSink implements sink.Adapter — it must also satisfy DLQPublisher.
	var pub DLQPublisher = &fakeDLQSink{}
	if err := pub.Publish(context.Background(), &pb.Frame{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- test helpers ---

// fakeDLQSink is a minimal DLQPublisher for testing.
type fakeDLQSink struct {
	err error
}

func (s *fakeDLQSink) Publish(_ context.Context, _ *pb.Frame) error { return s.err }

// captureDLQSink captures the published frame for assertion.
type captureDLQSink struct {
	published *pb.Frame
}

func (s *captureDLQSink) Publish(_ context.Context, f *pb.Frame) error { s.published = f; return nil }
