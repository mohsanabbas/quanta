package kafka

import (
	"sync"
	"testing"
)

func TestOffsetTracker_AckRemovesEntry(t *testing.T) {
	t.Parallel()

	ot := NewOffsetTracker(4)
	ot.Track(100, AckHandle{offset: 100, bytes: 64})

	if sz := ot.Size(); sz != 1 {
		t.Fatalf("Size after track: got %d, want 1", sz)
	}

	ot.Ack(100)

	if sz := ot.Size(); sz != 0 {
		t.Fatalf("Size after ack: got %d, want 0", sz)
	}
}

func TestOffsetTracker_Remove(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		trackOffset  int64
		removeOffset int64
		wantFound    bool
	}{
		{
			name:         "remove_existing_offset",
			trackOffset:  50,
			removeOffset: 50,
			wantFound:    true,
		},
		{
			name:         "remove_nonexistent_offset",
			trackOffset:  50,
			removeOffset: 99,
			wantFound:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ot := NewOffsetTracker(4)
			ot.Track(tt.trackOffset, AckHandle{offset: tt.trackOffset, bytes: 10})

			handle, ok := ot.Remove(tt.removeOffset)
			if ok != tt.wantFound {
				t.Fatalf("Remove found: got %v, want %v", ok, tt.wantFound)
			}
			if ok && handle.offset != tt.trackOffset {
				t.Fatalf("Remove handle offset: got %d, want %d", handle.offset, tt.trackOffset)
			}
			if ok {
				_, again := ot.Remove(tt.removeOffset)
				if again {
					t.Fatal("Remove must return ok=false on second call for same offset")
				}
			}
		})
	}
}

func TestOffsetTracker_ResetEmpty(t *testing.T) {
	t.Parallel()

	ot := NewOffsetTracker(4)
	handles := ot.Reset()
	if handles == nil {
		t.Fatal("Reset on empty tracker must return non-nil slice")
	}
	if len(handles) != 0 {
		t.Fatalf("Reset on empty tracker: got %d handles, want 0", len(handles))
	}
}

func TestOffsetTracker_DefaultCapacity(t *testing.T) {
	t.Parallel()

	ot := NewOffsetTracker(0)
	ot.Track(1, AckHandle{offset: 1, bytes: 8})
	if sz := ot.Size(); sz != 1 {
		t.Fatalf("Size after track with default capacity: got %d, want 1", sz)
	}
}

func TestOffsetTracker_ConcurrentTrackAck(t *testing.T) {
	t.Parallel()

	const n = 200
	ot := NewOffsetTracker(n)

	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(off int64) {
			defer wg.Done()
			ot.Track(off, AckHandle{offset: off, bytes: 1})
		}(int64(i))
	}
	wg.Wait()

	if sz := ot.Size(); sz != n {
		t.Fatalf("Size after concurrent track: got %d, want %d", sz, n)
	}

	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(off int64) {
			defer wg.Done()
			ot.Ack(off)
		}(int64(i))
	}
	wg.Wait()

	if sz := ot.Size(); sz != 0 {
		t.Fatalf("Size after concurrent ack: got %d, want 0", sz)
	}
}
