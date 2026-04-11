package pipeline

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	pb "quanta/api/proto/v1"
)

func kafkaTok(topic string, partition int32, offset int64) *pb.CheckpointToken {
	return &pb.CheckpointToken{Kind: &pb.CheckpointToken_Kafka{
		Kafka: &pb.KafkaOffset{Topic: topic, Partition: partition, Offset: offset},
	}}
}

func TestTokenKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		give *pb.CheckpointToken
		want string
	}{
		{
			name: "kafka",
			give: kafkaTok("orders", 3, 1000),
			want: "k:orders/3/1000",
		},
		{
			name: "sqs",
			give: &pb.CheckpointToken{Kind: &pb.CheckpointToken_Sqs{
				Sqs: &pb.SqsHandle{Queue: "q", Handle: "h"},
			}},
			want: "s:q/h",
		},
		{
			name: "http",
			give: &pb.CheckpointToken{Kind: &pb.CheckpointToken_Http{
				Http: &pb.HttpAckID{Id: "abc"},
			}},
			want: "h:abc",
		},
		{
			name: "raw",
			give: &pb.CheckpointToken{Kind: &pb.CheckpointToken_Raw{
				Raw: []byte("payload-id"),
			}},
			want: "r:payload-id",
		},
		{
			name: "nil_token",
			give: nil,
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tokenKey(tt.give)
			if got != tt.want {
				t.Fatalf("tokenKey: got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAckCoordinator_BarrierComplete(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		refs     int
		wantAcks int
	}{
		{name: "refs_1_single_complete", refs: 1, wantAcks: 1},
		{name: "refs_3_all_completed", refs: 3, wantAcks: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ac := &ackCounter{}
			coord := NewAckCoordinator()
			coord.Subscribe(ac.handler)

			tok := kafkaTok("t", 0, 100)
			b := coord.Barrier(tok, tt.refs)

			for i := 0; i < tt.refs; i++ {
				b.Complete()
			}

			if got := ac.get(); got != tt.wantAcks {
				t.Fatalf("commit count: got %d, want %d", got, tt.wantAcks)
			}
		})
	}
}

func TestAckCoordinator_BarrierAbortPreventsCommit(t *testing.T) {
	t.Parallel()

	ac := &ackCounter{}
	coord := NewAckCoordinator()
	coord.Subscribe(ac.handler)

	tok := kafkaTok("t", 0, 200)
	b := coord.Barrier(tok, 1)
	b.Abort()

	b.Complete()

	if got := ac.get(); got != 0 {
		t.Fatalf("commit count: got %d, want 0 (abort must prevent commit)", got)
	}
}

func TestAckCoordinator_AckResolvesBarrier(t *testing.T) {
	t.Parallel()

	ac := &ackCounter{}
	coord := NewAckCoordinator()
	coord.Subscribe(ac.handler)

	tok := kafkaTok("t", 1, 300)
	coord.Barrier(tok, 2)

	coord.Ack(context.Background(), tok)
	coord.Ack(context.Background(), tok)

	if got := ac.get(); got != 1 {
		t.Fatalf("commit count: got %d, want 1", got)
	}
}

func TestAckCoordinator_AckNoBarrierIsNoop(t *testing.T) {
	t.Parallel()

	ac := &ackCounter{}
	coord := NewAckCoordinator()
	coord.Subscribe(ac.handler)

	coord.Ack(context.Background(), kafkaTok("t", 0, 999))

	if got := ac.get(); got != 0 {
		t.Fatalf("commit count: got %d, want 0", got)
	}
}

func TestAckCoordinator_CommitNow(t *testing.T) {
	t.Parallel()

	ac := &ackCounter{}
	coord := NewAckCoordinator()
	coord.Subscribe(ac.handler)

	coord.CommitNow(kafkaTok("t", 0, 50))

	if got := ac.get(); got != 1 {
		t.Fatalf("commit count: got %d, want 1", got)
	}
}

func TestAckCoordinator_Fail(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		dlSet    bool
		wantDL   int
		wantAcks int
	}{
		{name: "with_dl_handler", dlSet: true, wantDL: 1, wantAcks: 0},
		{name: "no_dl_handler", dlSet: false, wantDL: 0, wantAcks: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ac := &ackCounter{}
			dl := &deadLetterCapture{}
			coord := NewAckCoordinator()
			coord.Subscribe(ac.handler)
			if tt.dlSet {
				coord.SetDeadLetter(dl.fn)
			}

			frame := &pb.Frame{
				Value:      []byte("bad"),
				Checkpoint: kafkaTok("t", 0, 77),
			}
			coord.Fail("stage-x", frame, errTest)

			if dl.len() != tt.wantDL {
				t.Fatalf("dl entries: got %d, want %d", dl.len(), tt.wantDL)
			}

			if got := ac.get(); got != tt.wantAcks {
				t.Fatalf("commit count: got %d, want %d", got, tt.wantAcks)
			}
		})
	}
}

var errTest = errorString("test error")

type errorString string

func (e errorString) Error() string { return string(e) }

func TestAckCoordinator_ConcurrentRelease(t *testing.T) {
	t.Parallel()

	var commitCount atomic.Int32
	coord := NewAckCoordinator()
	coord.Subscribe(func(*pb.ConnectorAck) {
		commitCount.Add(1)
	})

	const goroutines = 100
	tok := kafkaTok("t", 0, 500)
	coord.Barrier(tok, goroutines)

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			coord.Ack(context.Background(), tok)
		}()
	}
	wg.Wait()

	if got := commitCount.Load(); got != 1 {
		t.Fatalf("concurrent release: commit count %d, want exactly 1", got)
	}
}

func TestAckCoordinator_BarrierCleanedUp(t *testing.T) {
	t.Parallel()

	coord := NewAckCoordinator()
	tok := kafkaTok("t", 0, 600)

	coord.Barrier(tok, 1)

	if coord.Len() != 1 {
		t.Fatal("barrier should be present before completion")
	}

	coord.Ack(context.Background(), tok)

	if coord.Len() != 0 {
		t.Fatal("barrier should be removed after commit")
	}
}

func TestAckBarrier_UnderflowSafe(t *testing.T) {
	t.Parallel()

	ac := &ackCounter{}
	coord := NewAckCoordinator()
	coord.Subscribe(ac.handler)

	tok := kafkaTok("t", 0, 800)
	b := coord.Barrier(tok, 1)

	b.Complete()
	b.Complete()
	if got := ac.get(); got != 1 {
		t.Fatalf("commit count: got %d, want 1 (underflow must not double-commit)", got)
	}
}

func TestAckCoordinator_DuplicateBarrier(t *testing.T) {
	t.Parallel()

	ac := &ackCounter{}
	coord := NewAckCoordinator()
	coord.Subscribe(ac.handler)

	tok := kafkaTok("t", 0, 900)

	b1 := coord.Barrier(tok, 1)
	b2 := coord.Barrier(tok, 1)
	b1.Complete()
	if got := ac.get(); got != 0 {
		t.Fatalf("stale barrier committed: got %d, want 0", got)
	}

	b2.Complete()
	if got := ac.get(); got != 1 {
		t.Fatalf("new barrier: got %d commits, want 1", got)
	}

	if coord.Len() != 0 {
		t.Fatal("all barriers should be cleaned up")
	}
}

func TestAckCoordinator_NilToken(t *testing.T) {
	t.Parallel()

	t.Run("barrier_complete_noop", func(t *testing.T) {
		t.Parallel()
		ac := &ackCounter{}
		coord := NewAckCoordinator()
		coord.Subscribe(ac.handler)

		b := coord.Barrier(nil, 1)
		b.Complete()

		if got := ac.get(); got != 0 {
			t.Fatalf("nil-token barrier must not commit: got %d", got)
		}
		if coord.Len() != 0 {
			t.Fatal("nil-token barrier must not be tracked in map")
		}
	})

	t.Run("barrier_abort_noop", func(t *testing.T) {
		t.Parallel()
		coord := NewAckCoordinator()
		b := coord.Barrier(nil, 1)
		b.Abort() // must not panic
	})

	t.Run("ack_noop", func(t *testing.T) {
		t.Parallel()
		ac := &ackCounter{}
		coord := NewAckCoordinator()
		coord.Subscribe(ac.handler)

		coord.Ack(context.Background(), nil) // must not panic
		if got := ac.get(); got != 0 {
			t.Fatalf("nil ack must be noop: got %d", got)
		}
	})

	t.Run("commit_now_noop", func(t *testing.T) {
		t.Parallel()
		ac := &ackCounter{}
		coord := NewAckCoordinator()
		coord.Subscribe(ac.handler)

		coord.CommitNow(nil) // must not commit
		if got := ac.get(); got != 0 {
			t.Fatalf("nil CommitNow must be noop: got %d", got)
		}
	})
}

func TestAckBarrier_AbortThenRelease(t *testing.T) {
	t.Parallel()

	ac := &ackCounter{}
	coord := NewAckCoordinator()
	coord.Subscribe(ac.handler)

	tok := kafkaTok("t", 0, 1100)
	b := coord.Barrier(tok, 2)

	// Simulate one sink acks, then runner aborts due to another sink failure.
	b.Complete() // refs -> 1
	b.Abort()    // state aborted, barrier removed from map
	b.Complete() // refs -> 0, but state is aborted -> CAS fails -> no commit

	if got := ac.get(); got != 0 {
		t.Fatalf("aborted barrier must not commit: got %d", got)
	}
	if coord.Len() != 0 {
		t.Fatal("aborted barrier must be removed from map")
	}
}

func TestAckCoordinator_RemoveBarrierPointerSafe(t *testing.T) {
	t.Parallel()

	ac := &ackCounter{}
	coord := NewAckCoordinator()
	coord.Subscribe(ac.handler)

	tok := kafkaTok("t", 0, 1000)

	b1 := coord.Barrier(tok, 2)
	b2 := coord.Barrier(tok, 1)

	b1.Complete()
	if coord.Len() != 1 {
		t.Fatalf("B2 must still be in map: Len=%d", coord.Len())
	}

	b2.Complete()
	if got := ac.get(); got != 1 {
		t.Fatalf("B2 commit: got %d, want 1", got)
	}
	if coord.Len() != 0 {
		t.Fatal("all barriers should be cleaned up")
	}
}

func TestAckCoordinator_Len(t *testing.T) {
	t.Parallel()

	coord := NewAckCoordinator()
	if coord.Len() != 0 {
		t.Fatal("empty coordinator must have Len=0")
	}

	tok1 := kafkaTok("t", 0, 1)
	tok2 := kafkaTok("t", 0, 2)
	tok3 := kafkaTok("t", 0, 3)

	coord.Barrier(tok1, 1)
	coord.Barrier(tok2, 1)
	coord.Barrier(tok3, 1)

	if coord.Len() != 3 {
		t.Fatalf("Len: got %d, want 3", coord.Len())
	}

	coord.Ack(context.Background(), tok1)
	if coord.Len() != 2 {
		t.Fatalf("Len after 1 ack: got %d, want 2", coord.Len())
	}

	coord.Ack(context.Background(), tok2)
	coord.Ack(context.Background(), tok3)
	if coord.Len() != 0 {
		t.Fatalf("Len after all acks: got %d, want 0", coord.Len())
	}
}

func TestAckCoordinator_FailNeverCommits(t *testing.T) {
	t.Parallel()

	ac := &ackCounter{}
	dl := &deadLetterCapture{}
	coord := NewAckCoordinator()
	coord.Subscribe(ac.handler)
	coord.SetDeadLetter(dl.fn)

	tok := kafkaTok("t", 0, 1200)
	coord.Barrier(tok, 1)

	frame := &pb.Frame{Value: []byte("bad"), Checkpoint: tok}
	coord.Fail("stage-x", frame, errTest)

	if dl.len() != 1 {
		t.Fatalf("dl entries: got %d, want 1", dl.len())
	}
	if got := ac.get(); got != 0 {
		t.Fatalf("Fail must not commit: got %d, want 0", got)
	}
	if coord.Len() != 1 {
		t.Fatalf("barrier must remain until pushFrame resolves: Len=%d", coord.Len())
	}
}
