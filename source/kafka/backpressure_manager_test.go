package kafka

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestBackpressureManager_AcquireRelease(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		newMgr      func() BackpressureManager
		acquireSize int64
		acquireN    int
		wantBlock   bool
	}{
		{
			name:        "count_based_within_limit",
			newMgr:      func() BackpressureManager { return NewCountBasedBackpressureManager(3) },
			acquireSize: 999,
			acquireN:    3,
			wantBlock:   false,
		},
		{
			name:        "count_based_exceeds_limit",
			newMgr:      func() BackpressureManager { return NewCountBasedBackpressureManager(2) },
			acquireSize: 1,
			acquireN:    3,
			wantBlock:   true,
		},
		{
			name:        "size_based_within_limit",
			newMgr:      func() BackpressureManager { return NewSizeBasedBackpressureManager(100) },
			acquireSize: 30,
			acquireN:    3,
			wantBlock:   false,
		},
		{
			name:        "size_based_exceeds_limit",
			newMgr:      func() BackpressureManager { return NewSizeBasedBackpressureManager(50) },
			acquireSize: 30,
			acquireN:    2,
			wantBlock:   true,
		},
		{
			name:        "combined_within_both_limits",
			newMgr:      func() BackpressureManager { return NewCombinedBackpressureManager(100, 3) },
			acquireSize: 20,
			acquireN:    3,
			wantBlock:   false,
		},
		{
			name:        "combined_msg_limit_hit",
			newMgr:      func() BackpressureManager { return NewCombinedBackpressureManager(1000, 1) },
			acquireSize: 10,
			acquireN:    2,
			wantBlock:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mgr := tt.newMgr()

			for i := 0; i < tt.acquireN-1; i++ {
				if err := mgr.Acquire(context.Background(), tt.acquireSize); err != nil {
					t.Fatalf("Acquire[%d]: %v", i, err)
				}
			}

			if !tt.wantBlock {
				if err := mgr.Acquire(context.Background(), tt.acquireSize); err != nil {
					t.Fatalf("final Acquire: %v", err)
				}
				for i := 0; i < tt.acquireN; i++ {
					mgr.Release(tt.acquireSize)
				}
				return
			}

			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()

			err := mgr.Acquire(ctx, tt.acquireSize)
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("expected DeadlineExceeded, got: %v", err)
			}

			for i := 0; i < tt.acquireN-1; i++ {
				mgr.Release(tt.acquireSize)
			}

			if err := mgr.Acquire(context.Background(), tt.acquireSize); err != nil {
				t.Fatalf("Acquire after release: %v", err)
			}
			mgr.Release(tt.acquireSize)
		})
	}
}

func TestBackpressureManager_Capacity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mgr     BackpressureManager
		wantCap int64
	}{
		{
			name:    "count_based_default",
			mgr:     NewCountBasedBackpressureManager(0),
			wantCap: 1000,
		},
		{
			name:    "count_based_explicit",
			mgr:     NewCountBasedBackpressureManager(42),
			wantCap: 42,
		},
		{
			name:    "size_based_default",
			mgr:     NewSizeBasedBackpressureManager(0),
			wantCap: _defaultMaxBytes,
		},
		{
			name:    "size_based_explicit",
			mgr:     NewSizeBasedBackpressureManager(1024),
			wantCap: 1024,
		},
		{
			name:    "combined_returns_byte_cap",
			mgr:     NewCombinedBackpressureManager(2048, 100),
			wantCap: 2048,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.mgr.Capacity(); got != tt.wantCap {
				t.Fatalf("Capacity: got %d, want %d", got, tt.wantCap)
			}
		})
	}
}

func TestBackpressureManager_ZeroSizeNormalisedToOne(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		mgr  BackpressureManager
	}{
		{"size_based", NewSizeBasedBackpressureManager(10)},
		{"combined", NewCombinedBackpressureManager(10, 10)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if err := tt.mgr.Acquire(context.Background(), 0); err != nil {
				t.Fatalf("Acquire(0): %v", err)
			}

			tt.mgr.Release(0)
		})
	}
}
