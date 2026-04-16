package stdout

import (
	"context"
	"errors"
	"fmt"
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

func TestDriver_Configure_ValidConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		cfg           Config
		wantBatchSize int
	}{
		{name: "value_config", cfg: Config{BatchSize: 4, PrintCounter: true}, wantBatchSize: 4},
		{name: "pointer_config_via_value", cfg: Config{BatchSize: 2}, wantBatchSize: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			d := &driver{}
			if err := d.Configure(context.Background(), tt.cfg); err != nil {
				t.Fatalf("Configure: %v", err)
			}
			if d.cfg.BatchSize != tt.wantBatchSize {
				t.Fatalf("BatchSize: got %d, want %d", d.cfg.BatchSize, tt.wantBatchSize)
			}
		})
	}
}

func TestDriver_Configure_ValidPointerConfig(t *testing.T) {
	t.Parallel()

	d := &driver{}
	if err := d.Configure(context.Background(), &Config{BatchSize: 2}); err != nil {
		t.Fatalf("Configure with *Config: %v", err)
	}
	if d.cfg.BatchSize != 2 {
		t.Fatalf("BatchSize: got %d, want 2", d.cfg.BatchSize)
	}
}

func TestDriver_Configure_InvalidType(t *testing.T) {
	t.Parallel()

	d := &driver{}
	if err := d.Configure(context.Background(), "not-a-config"); err == nil {
		t.Fatal("Configure with wrong type must return error")
	}
}

func TestDriver_Configure_DefaultValueMaxBytes(t *testing.T) {
	t.Parallel()

	d := &driver{}
	if err := d.Configure(context.Background(), Config{ValueMaxBytes: 0}); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	if d.cfg.ValueMaxBytes != 120 {
		t.Fatalf("ValueMaxBytes default: got %d, want 120", d.cfg.ValueMaxBytes)
	}
}

func TestDriver_Publish_BatchFlush(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		batchSize int
		publish   int
		wantAcks  int
	}{
		{name: "batch_of_3_flushes_on_third", batchSize: 3, publish: 3, wantAcks: 3},
		{name: "batch_not_reached_no_flush", batchSize: 5, publish: 3, wantAcks: 0},
		{name: "batch_0_disabled_no_auto_flush", batchSize: 0, publish: 4, wantAcks: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			d := &driver{}
			if err := d.Configure(context.Background(), Config{BatchSize: tt.batchSize}); err != nil {
				t.Fatalf("Configure: %v", err)
			}

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
	if err := d.Configure(context.Background(), Config{BatchSize: 10}); err != nil {
		t.Fatalf("Configure: %v", err)
	}

	var mu sync.Mutex
	var acked int
	d.BindAck(func(_ context.Context, _ *pb.CheckpointToken) {
		mu.Lock()
		acked++
		mu.Unlock()
	})

	for i := 0; i < 3; i++ {
		if err := d.Publish(context.Background(), makeFrame(makeToken("t"))); err != nil {
			t.Fatalf("Publish[%d]: %v", i, err)
		}
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
	if err := d.Configure(context.Background(), Config{BatchSize: 10}); err != nil {
		t.Fatalf("Configure: %v", err)
	}

	if err := d.Publish(context.Background(), makeFrame(makeToken("t"))); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	if err := d.Close(context.Background()); err != nil {
		t.Fatalf("Close without ackFn: %v", err)
	}
}

func TestDriver_Publish_ContextCancelled_ReturnsError(t *testing.T) {
	t.Parallel()

	d := &driver{}
	if err := d.Configure(context.Background(), Config{DelayMS: 100}); err != nil {
		t.Fatalf("Configure: %v", err)
	}

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
	if err := d.Configure(context.Background(), Config{BatchSize: n}); err != nil {
		t.Fatalf("Configure: %v", err)
	}

	var mu sync.Mutex
	var ackedTokens []*pb.CheckpointToken
	d.BindAck(func(_ context.Context, tok *pb.CheckpointToken) {
		mu.Lock()
		ackedTokens = append(ackedTokens, tok)
		mu.Unlock()
	})

	sent := make([]*pb.CheckpointToken, n)
	for i := 0; i < n; i++ {
		tok := makeToken(fmt.Sprintf("id-%d", i))
		sent[i] = tok
		if err := d.Publish(context.Background(), makeFrame(tok)); err != nil {
			t.Fatalf("Publish[%d]: %v", i, err)
		}
	}

	mu.Lock()
	gotN := len(ackedTokens)
	mu.Unlock()

	if gotN != n {
		t.Fatalf("acked count: got %d, want %d", gotN, n)
	}

	for i := 0; i < n; i++ {
		if ackedTokens[i] != sent[i] {
			t.Fatalf("acked[%d]: got different pointer, ack order not preserved", i)
		}
	}
}

func TestDriver_Publish_DelayRespected(t *testing.T) {
	t.Parallel()

	d := &driver{}
	if err := d.Configure(context.Background(), Config{DelayMS: 20}); err != nil {
		t.Fatalf("Configure: %v", err)
	}
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
