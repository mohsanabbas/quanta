package stdout

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	pb "quanta/api/proto/v1"
	"quanta/sink"
)

func makeToken(id string) *pb.CheckpointToken {
	return &pb.CheckpointToken{Kind: &pb.CheckpointToken_Raw{Raw: []byte(id)}}
}

func makeFrame(token *pb.CheckpointToken) *pb.Frame {
	return &pb.Frame{Value: []byte("data"), Checkpoint: token}
}

// ---------------------------------------------------------------------------
// Configure
// ---------------------------------------------------------------------------

func TestDriver_Configure_ValidConfig(t *testing.T) {
	t.Parallel()

	d := &driver{}
	err := d.Configure(context.Background(), Config{BatchSize: 4, PrintCounter: true})
	if err != nil {
		t.Fatalf("Configure: unexpected error: %v", err)
	}
	if d.cfg.BatchSize != 4 {
		t.Fatalf("BatchSize: got %d, want 4", d.cfg.BatchSize)
	}
}

func TestDriver_Configure_ValidPointerConfig(t *testing.T) {
	t.Parallel()

	d := &driver{}
	err := d.Configure(context.Background(), &Config{BatchSize: 2})
	if err != nil {
		t.Fatalf("Configure with *Config: unexpected error: %v", err)
	}
	if d.cfg.BatchSize != 2 {
		t.Fatalf("BatchSize: got %d, want 2", d.cfg.BatchSize)
	}
}

func TestDriver_Configure_InvalidType(t *testing.T) {
	t.Parallel()

	d := &driver{}
	err := d.Configure(context.Background(), "not-a-config")
	if err == nil {
		t.Fatal("Configure with wrong type must return error")
	}
}

func TestDriver_Configure_DefaultValueMaxBytes(t *testing.T) {
	t.Parallel()

	d := &driver{}
	_ = d.Configure(context.Background(), Config{ValueMaxBytes: 0})
	if d.cfg.ValueMaxBytes != 120 {
		t.Fatalf("ValueMaxBytes default: got %d, want 120", d.cfg.ValueMaxBytes)
	}
}

// ---------------------------------------------------------------------------
// Publish + BindAck
// ---------------------------------------------------------------------------

func TestDriver_Publish_BatchFlush(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		batchSize int
		publish   int
		wantAcks  int
	}{
		{
			name:      "batch_of_3_flushes_on_third",
			batchSize: 3,
			publish:   3,
			wantAcks:  3,
		},
		{
			name:      "batch_not_reached_no_flush",
			batchSize: 5,
			publish:   3,
			wantAcks:  0,
		},
		{
			name:      "batch_0_disabled_no_auto_flush",
			batchSize: 0,
			publish:   4,
			wantAcks:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			d := &driver{}
			_ = d.Configure(context.Background(), Config{BatchSize: tt.batchSize})

			var mu sync.Mutex
			var acked int
			d.BindAck(func(_ context.Context, _ *pb.CheckpointToken) {
				mu.Lock()
				acked++
				mu.Unlock()
			})

			for i := 0; i < tt.publish; i++ {
				if err := d.Publish(context.Background(), makeFrame(makeToken("t"))); err != nil {
					t.Fatalf("Publish[%d]: %v", i, err)
				}
			}

			mu.Lock()
			got := acked
			mu.Unlock()

			if got != tt.wantAcks {
				t.Fatalf("acked tokens: got %d, want %d", got, tt.wantAcks)
			}
		})
	}
}

func TestDriver_Close_FlushesPending(t *testing.T) {
	t.Parallel()

	d := &driver{}
	_ = d.Configure(context.Background(), Config{BatchSize: 10})

	var mu sync.Mutex
	var acked int
	d.BindAck(func(_ context.Context, _ *pb.CheckpointToken) {
		mu.Lock()
		acked++
		mu.Unlock()
	})

	for i := 0; i < 3; i++ {
		_ = d.Publish(context.Background(), makeFrame(makeToken("t")))
	}

	if err := d.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}

	mu.Lock()
	got := acked
	mu.Unlock()

	if got != 3 {
		t.Fatalf("acked on Close: got %d, want 3", got)
	}
}

func TestDriver_Close_NoAckFn_NoError(t *testing.T) {
	t.Parallel()

	d := &driver{}
	_ = d.Configure(context.Background(), Config{BatchSize: 10})

	_ = d.Publish(context.Background(), makeFrame(makeToken("t")))

	if err := d.Close(context.Background()); err != nil {
		t.Fatalf("Close without ackFn: %v", err)
	}
}

func TestDriver_Publish_ContextCancelled_ReturnsError(t *testing.T) {
	t.Parallel()

	d := &driver{}
	_ = d.Configure(context.Background(), Config{DelayMS: 100})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := d.Publish(ctx, makeFrame(makeToken("t")))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Publish with cancelled context: got %v, want context.Canceled", err)
	}
}

func TestDriver_BindAck_InterfaceConformance(t *testing.T) {
	t.Parallel()

	d := &driver{}
	var _ sink.Adapter = d
	var _ sink.AckAware = d
}

func TestDriver_Publish_MultipleFrames_AckedInOrder(t *testing.T) {
	t.Parallel()

	const n = 5
	d := &driver{}
	_ = d.Configure(context.Background(), Config{BatchSize: n})

	var mu sync.Mutex
	var tokens []*pb.CheckpointToken
	d.BindAck(func(_ context.Context, tok *pb.CheckpointToken) {
		mu.Lock()
		tokens = append(tokens, tok)
		mu.Unlock()
	})

	expected := make([]*pb.CheckpointToken, n)
	for i := 0; i < n; i++ {
		tok := makeToken("t")
		expected[i] = tok
		_ = d.Publish(context.Background(), makeFrame(tok))
	}

	mu.Lock()
	got := len(tokens)
	mu.Unlock()

	if got != n {
		t.Fatalf("acked tokens: got %d, want %d", got, n)
	}
}

func TestDriver_Publish_DelayRespected(t *testing.T) {
	t.Parallel()

	d := &driver{}
	_ = d.Configure(context.Background(), Config{DelayMS: 20})
	d.BindAck(func(context.Context, *pb.CheckpointToken) {})

	start := time.Now()
	if err := d.Publish(context.Background(), makeFrame(makeToken("t"))); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	elapsed := time.Since(start)

	if elapsed < 15*time.Millisecond {
		t.Fatalf("delay not respected: elapsed %v, want >= 15ms", elapsed)
	}
}
