package kafka

import (
	"sync"
	"testing"
)

func TestPartitionTracker_AckAdvance(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		windowBits  uint32
		resetBase   int64
		reserve     []int64
		ackSequence []int64
		wantBases   []int64
		wantAdv     []bool
	}{
		{
			name:        "in_order_advance",
			windowBits:  8,
			resetBase:   100,
			reserve:     []int64{100, 101, 102},
			ackSequence: []int64{100, 101, 102},
			wantBases:   []int64{101, 102, 103},
			wantAdv:     []bool{true, true, true},
		},
		{
			name:        "out_of_order_blocks_until_base_acked",
			windowBits:  8,
			resetBase:   100,
			reserve:     []int64{100, 101, 102},
			ackSequence: []int64{101, 100},
			wantBases:   []int64{100, 102},
			wantAdv:     []bool{false, true},
		},
		{
			name:        "ack_beyond_window_ignored",
			windowBits:  4,
			resetBase:   50,
			reserve:     []int64{50},
			ackSequence: []int64{80},
			wantBases:   []int64{50},
			wantAdv:     []bool{false},
		},
		{
			name:        "ack_below_base_ignored",
			windowBits:  8,
			resetBase:   100,
			reserve:     []int64{100},
			ackSequence: []int64{99},
			wantBases:   []int64{100},
			wantAdv:     []bool{false},
		},
		{
			name:        "duplicate_ack_no_double_advance",
			windowBits:  8,
			resetBase:   100,
			reserve:     []int64{100},
			ackSequence: []int64{100, 100},
			wantBases:   []int64{101, 101},
			wantAdv:     []bool{true, false},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tracker := NewPartitionTracker(tt.windowBits)
			tracker.Reset(tt.resetBase)

			for _, off := range tt.reserve {
				tracker.Reserve(off)
			}

			for i, off := range tt.ackSequence {
				base, adv := tracker.AckOffset(off)
				if base != tt.wantBases[i] {
					t.Fatalf("AckOffset(%d)[%d]: base got %d, want %d", off, i, base, tt.wantBases[i])
				}
				if adv != tt.wantAdv[i] {
					t.Fatalf("AckOffset(%d)[%d]: adv got %v, want %v", off, i, adv, tt.wantAdv[i])
				}
			}
		})
	}
}

func TestPartitionTracker_ReserveOverflow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		windowBits uint32
		resetBase  int64
		fill       int
		overflow   int64
		wantSlot   uint32
	}{
		{
			name:       "within_window",
			windowBits: 4,
			resetBase:  10,
			fill:       3,
			overflow:   13,
			wantSlot:   3,
		},
		{
			name:       "at_boundary_returns_invalid",
			windowBits: 4,
			resetBase:  10,
			fill:       4,
			overflow:   14,
			wantSlot:   InvalidSlot,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tracker := NewPartitionTracker(tt.windowBits)
			tracker.Reset(tt.resetBase)

			for i := 0; i < tt.fill; i++ {
				tracker.Reserve(tt.resetBase + int64(i))
			}

			got := tracker.Reserve(tt.overflow)
			if got != tt.wantSlot {
				t.Fatalf("Reserve(%d): got %d, want %d", tt.overflow, got, tt.wantSlot)
			}
		})
	}
}

func TestPartitionTracker_AutoInitOnReserve(t *testing.T) {
	t.Parallel()

	tracker := NewPartitionTracker(8)

	if tracker.Initialized() {
		t.Fatal("should not be initialized before first Reserve")
	}

	slot := tracker.Reserve(42)
	if slot != 0 {
		t.Fatalf("first Reserve should return slot 0, got %d", slot)
	}
	if !tracker.Initialized() {
		t.Fatal("should be initialized after first Reserve")
	}
	if tracker.Base() != 42 {
		t.Fatalf("base should be 42 after first Reserve, got %d", tracker.Base())
	}
}

func TestPartitionTracker_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	tracker := NewPartitionTracker(4096)
	tracker.Reset(0)

	const workers = 8
	const opsPerWorker = 500

	var wg sync.WaitGroup
	wg.Add(workers * 2)

	for w := 0; w < workers; w++ {
		go func(base int) {
			defer wg.Done()
			for i := 0; i < opsPerWorker; i++ {
				tracker.Reserve(int64(base*opsPerWorker + i))
			}
		}(w)
	}

	for w := 0; w < workers; w++ {
		go func(base int) {
			defer wg.Done()
			for i := 0; i < opsPerWorker; i++ {
				tracker.AckOffset(int64(base*opsPerWorker + i))
			}
		}(w)
	}

	wg.Wait()

	base := tracker.Base()
	if base < 0 {
		t.Fatalf("base should be non-negative after operations, got %d", base)
	}
}

func TestOffsetTracker_TrackAndAck(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		trackOff   int64
		trackBytes int64
		ackOff     int64
		wantOK     bool
	}{
		{
			name:       "ack_tracked_offset",
			trackOff:   10,
			trackBytes: 42,
			ackOff:     10,
			wantOK:     true,
		},
		{
			name:       "ack_untracked_offset",
			trackOff:   10,
			trackBytes: 42,
			ackOff:     99,
			wantOK:     false,
		},
		{
			name:       "double_ack_second_returns_false",
			trackOff:   10,
			trackBytes: 42,
			ackOff:     10,
			wantOK:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ot := NewOffsetTracker(4)
			handle := AckHandle{offset: tt.trackOff, bytes: tt.trackBytes}
			ot.Track(tt.trackOff, handle)

			h, ok := ot.Ack(tt.ackOff)
			if ok != tt.wantOK {
				t.Fatalf("Ack(%d): ok got %v, want %v", tt.ackOff, ok, tt.wantOK)
			}
			if ok {
				if h.offset != tt.trackOff || h.bytes != tt.trackBytes {
					t.Fatalf("handle: got %+v, want offset=%d bytes=%d", h, tt.trackOff, tt.trackBytes)
				}

				_, ok2 := ot.Ack(tt.ackOff)
				if ok2 {
					t.Fatal("second Ack should return false")
				}
			}
		})
	}
}

func TestOffsetTracker_ClosePreventsTracking(t *testing.T) {
	t.Parallel()

	ot := NewOffsetTracker(4)
	ot.Track(1, AckHandle{offset: 1, bytes: 10})
	ot.Close()

	ot2 := NewOffsetTracker(4)
	ot2.Close()
	ot2.Track(2, AckHandle{offset: 2, bytes: 20})
	if ot2.Size() != 0 {
		t.Fatalf("Size after close+track: got %d, want 0", ot2.Size())
	}
}

func TestOffsetTracker_ResetReturnsPending(t *testing.T) {
	t.Parallel()

	ot := NewOffsetTracker(4)
	ot.Track(10, AckHandle{offset: 10, bytes: 100})
	ot.Track(11, AckHandle{offset: 11, bytes: 200})

	handles := ot.Reset()
	if len(handles) != 2 {
		t.Fatalf("Reset: got %d handles, want 2", len(handles))
	}
	if ot.Size() != 0 {
		t.Fatalf("Size after Reset: got %d, want 0", ot.Size())
	}
}
