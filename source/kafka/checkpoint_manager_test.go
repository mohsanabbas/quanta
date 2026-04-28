package kafka

import (
	"errors"
	"testing"
	"time"
)

func TestSlidingWindowCheckpointManager_TrackAndAck(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		windowBits  uint32
		capacity    int
		trackOffset int64
		trackSize   int64
		ackOffset   int64
		wantHandle  AckHandle
		wantBase    int64
		wantAdv     bool
	}{
		{
			name:        "track_then_ack_advances_base",
			windowBits:  8,
			capacity:    4,
			trackOffset: 100,
			trackSize:   5,
			ackOffset:   100,
			wantHandle:  AckHandle{offset: 100, bytes: 5},
			wantBase:    101,
			wantAdv:     true,
		},
		{
			name:        "ack_unknown_offset_does_not_advance",
			windowBits:  8,
			capacity:    4,
			trackOffset: 100,
			trackSize:   5,
			ackOffset:   500,
			wantHandle:  AckHandle{},
			wantBase:    100,
			wantAdv:     false,
		},
		{
			name:        "zero_size_normalised_to_one",
			windowBits:  8,
			capacity:    4,
			trackOffset: 200,
			trackSize:   0,
			ackOffset:   200,
			wantHandle:  AckHandle{offset: 200, bytes: 1},
			wantBase:    201,
			wantAdv:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mgr := NewSlidingWindowCheckpointManager(tt.windowBits, tt.capacity)
			if err := mgr.Track(t.Context(), tt.trackOffset, tt.trackSize); err != nil {
				t.Fatalf("Track: %v", err)
			}

			handle, base, adv := mgr.Ack(tt.ackOffset)
			if handle != tt.wantHandle {
				t.Fatalf("handle: got %+v, want %+v", handle, tt.wantHandle)
			}
			if base != tt.wantBase {
				t.Fatalf("base: got %d, want %d", base, tt.wantBase)
			}
			if adv != tt.wantAdv {
				t.Fatalf("advanced: got %v, want %v", adv, tt.wantAdv)
			}
		})
	}
}

func TestSlidingWindowCheckpointManager_BoundedBackoff(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		window  uint32
		fill    int
		nextOff int64
		wantErr error
	}{
		{
			name:    "within_window_succeeds",
			window:  8,
			fill:    7,
			nextOff: 7,
			wantErr: nil,
		},
		{
			name:    "window_full_returns_exhausted",
			window:  256,
			fill:    256,
			nextOff: 256,
			wantErr: ErrWindowExhausted,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mgr := NewSlidingWindowCheckpointManager(tt.window, tt.fill+1)

			for i := 0; i < tt.fill; i++ {
				if err := mgr.Track(t.Context(), int64(i), 1); err != nil {
					t.Fatalf("Track(%d): %v", i, err)
				}
			}

			err := mgr.Track(t.Context(), tt.nextOff, 1)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Track(%d): got %v, want %v", tt.nextOff, err, tt.wantErr)
			}
		})
	}
}

func TestApplicationControlledCheckpointManager_Lifecycle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		capacity     int64
		tracks       []int64
		ackOrder     []int64
		wantBases    []int64
		wantAdvanced []bool
	}{
		{
			name:         "sequential_ack_advances_base",
			capacity:     10,
			tracks:       []int64{10, 11, 12},
			ackOrder:     []int64{10, 11, 12},
			wantBases:    []int64{11, 12, 13},
			wantAdvanced: []bool{true, true, true},
		},
		{
			name:         "out_of_order_ack_partial_advance",
			capacity:     10,
			tracks:       []int64{10, 11, 12},
			ackOrder:     []int64{12, 11, 10},
			wantBases:    []int64{10, 10, 11},
			wantAdvanced: []bool{false, false, true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mgr := NewApplicationControlledCheckpointManager(tt.capacity)

			for _, off := range tt.tracks {
				if err := mgr.Track(t.Context(), off, 1); err != nil {
					t.Fatalf("Track(%d): %v", off, err)
				}
			}

			for i, off := range tt.ackOrder {
				_, base, adv := mgr.Ack(off)
				if base != tt.wantBases[i] {
					t.Fatalf("Ack(%d): base got %d, want %d", off, base, tt.wantBases[i])
				}
				if adv != tt.wantAdvanced[i] {
					t.Fatalf("Ack(%d): advanced got %v, want %v", off, adv, tt.wantAdvanced[i])
				}
			}
		})
	}
}

func TestApplicationControlledCheckpointManager_BlocksUntilAck(t *testing.T) {
	t.Parallel()

	mgr := NewApplicationControlledCheckpointManager(1)
	if err := mgr.Track(t.Context(), 10, 7); err != nil {
		t.Fatalf("Track: %v", err)
	}

	done := make(chan struct{})
	go func() {
		if err := mgr.Track(t.Context(), 11, 9); err != nil {
			t.Errorf("Track(11): %v", err)
		}
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("second Track should block until ack")
	case <-time.After(30 * time.Millisecond):
	}

	handle, base, adv := mgr.Ack(10)
	if handle.offset != 10 || handle.bytes != 7 {
		t.Fatalf("unexpected handle: %+v", handle)
	}
	if !adv || base != 11 {
		t.Fatalf("expected base=11 advanced=true, got base=%d advanced=%v", base, adv)
	}

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("second Track should unblock after ack")
	}
}

func TestApplicationControlledCheckpointManager_ClosedReturnsError(t *testing.T) {
	t.Parallel()

	mgr := NewApplicationControlledCheckpointManager(10)
	mgr.Close()

	err := mgr.Track(t.Context(), 1, 1)
	if !errors.Is(err, ErrCheckpointClosed) {
		t.Fatalf("expected ErrCheckpointClosed, got: %v", err)
	}
}

func TestApplicationControlledCheckpointManager_ResetClearsState(t *testing.T) {
	t.Parallel()

	mgr := NewApplicationControlledCheckpointManager(10)

	if err := mgr.Track(t.Context(), 50, 10); err != nil {
		t.Fatalf("Track: %v", err)
	}
	if err := mgr.Track(t.Context(), 51, 20); err != nil {
		t.Fatalf("Track: %v", err)
	}

	handles := mgr.Reset()
	if len(handles) != 2 {
		t.Fatalf("Reset handles: got %d, want 2", len(handles))
	}
	if mgr.Initialized() {
		t.Fatal("expected Initialized=false after Reset")
	}
	if mgr.Base() != -1 {
		t.Fatalf("expected base=-1 after Reset, got %d", mgr.Base())
	}
}
