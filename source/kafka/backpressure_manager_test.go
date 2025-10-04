package kafka

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCountBasedBackpressureIgnoresSize(t *testing.T) {
	mgr := NewCountBasedBackpressureManager(2)

	if err := mgr.Acquire(context.Background(), 128); err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}
	if err := mgr.Acquire(context.Background(), 256); err != nil {
		t.Fatalf("second acquire failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := mgr.Acquire(ctx, 1); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}

	mgr.Release(0)
	mgr.Release(0)

	if err := mgr.Acquire(context.Background(), 1); err != nil {
		t.Fatalf("acquire after release failed: %v", err)
	}
	mgr.Release(1)
}

func TestCombinedBackpressureEnforcesMessageLimit(t *testing.T) {
	mgr := NewCombinedBackpressureManager(100, 1)

	if err := mgr.Acquire(context.Background(), 20); err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := mgr.Acquire(ctx, 30); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded waiting on message tokens, got %v", err)
	}

	mgr.Release(20)

	if err := mgr.Acquire(context.Background(), 40); err != nil {
		t.Fatalf("acquire after release failed: %v", err)
	}
	mgr.Release(40)
}
