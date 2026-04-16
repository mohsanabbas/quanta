package checkpoint

import (
	"context"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Uncapped
// ---------------------------------------------------------------------------

func TestUncapped_PendingEmpty(t *testing.T) {
	t.Parallel()

	u := NewUncapped[string]()
	if p := u.Pending(); p != 0 {
		t.Fatalf("Pending on empty: got %d, want 0", p)
	}
}

func TestUncapped_HighestNilBeforeAnyResolve(t *testing.T) {
	t.Parallel()

	u := NewUncapped[int]()
	u.Track(42, 10)
	if h := u.Highest(); h != nil {
		t.Fatal("Highest must be nil before any resolve")
	}
}

func TestUncapped_SingleTrackResolve(t *testing.T) {
	t.Parallel()

	u := NewUncapped[int]()
	resolve := u.Track(7, 10)

	if p := u.Pending(); p != 10 {
		t.Fatalf("Pending after track: got %d, want 10", p)
	}

	h := resolve()
	if h == nil || *h != 7 {
		t.Fatalf("resolve: got %v, want 7", h)
	}
	if p := u.Pending(); p != 0 {
		t.Fatalf("Pending after resolve: got %d, want 0", p)
	}
}

func TestUncapped_InOrderResolve(t *testing.T) {
	t.Parallel()

	u := NewUncapped[int]()
	r1 := u.Track(10, 5)
	r2 := u.Track(20, 5)
	r3 := u.Track(30, 5)

	h1 := r1()
	if h1 == nil || *h1 != 10 {
		t.Fatalf("after r1: got %v, want 10", h1)
	}

	h2 := r2()
	if h2 == nil || *h2 != 20 {
		t.Fatalf("after r2: got %v, want 20", h2)
	}

	h3 := r3()
	if h3 == nil || *h3 != 30 {
		t.Fatalf("after r3: got %v, want 30", h3)
	}

	if p := u.Pending(); p != 0 {
		t.Fatalf("Pending after all resolved: got %d, want 0", p)
	}
}

func TestUncapped_OutOfOrderResolveHeadLast(t *testing.T) {
	t.Parallel()

	// Resolve last → middle → head. The checkpoint only advances when the
	// head resolves, at which point Highest reports the max payload seen.
	u := NewUncapped[int]()
	r1 := u.Track(1, 10)
	r2 := u.Track(2, 10)
	r3 := u.Track(3, 10)

	// Resolve tail and middle first: checkpoint stays nil.
	r3()
	r2()
	if h := u.Highest(); h != nil {
		t.Fatalf("Highest before head resolved: got %v, want nil", h)
	}

	// Resolve head: checkpoint advances and reports the accumulated payload.
	h := r1()
	if h == nil || *h != 3 {
		t.Fatalf("Highest after head resolved (out of order): got %v, want 3", h)
	}
	if p := u.Pending(); p != 0 {
		t.Fatalf("Pending after all resolved: got %d, want 0", p)
	}
}

func TestUncapped_HeadResolveAdvancesOnly(t *testing.T) {
	t.Parallel()

	u := NewUncapped[int]()
	r1 := u.Track(1, 5)
	_ = u.Track(2, 5)
	_ = u.Track(3, 5)

	// Resolve head: checkpoint advances to payload=1.
	h := r1()
	if h == nil || *h != 1 {
		t.Fatalf("after head resolve: got %v, want 1", h)
	}
	// Pending: remaining two nodes still tracked.
	if p := u.Pending(); p != 10 {
		t.Fatalf("Pending after head resolve: got %d, want 10", p)
	}
}

func TestUncapped_PendingSumsSizes(t *testing.T) {
	t.Parallel()

	u := NewUncapped[string]()
	u.Track("a", 10)
	u.Track("b", 20)
	u.Track("c", 30)

	if p := u.Pending(); p != 60 {
		t.Fatalf("Pending: got %d, want 60", p)
	}
}

// ---------------------------------------------------------------------------
// Capped
// ---------------------------------------------------------------------------

func TestCapped_TrackWithinCapacityDoesNotBlock(t *testing.T) {
	t.Parallel()

	c := NewCapped[int](100)

	for i := 0; i < 3; i++ {
		done := make(chan error, 1)
		go func(idx int) {
			_, err := c.Track(context.Background(), idx, 30)
			done <- err
		}(i)

		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Track[%d]: %v", i, err)
			}
		case <-time.After(200 * time.Millisecond):
			t.Fatalf("Track[%d] blocked unexpectedly within capacity", i)
		}
	}
}

func TestCapped_TrackBlocksWhenCapacityExceeded(t *testing.T) {
	t.Parallel()

	c := NewCapped[int](10)

	r1, err := c.Track(context.Background(), 1, 10)
	if err != nil {
		t.Fatalf("first Track: %v", err)
	}

	ready := make(chan struct{})
	done := make(chan struct{})
	go func() {
		close(ready)
		c.Track(context.Background(), 2, 10) //nolint:errcheck
		close(done)
	}()

	<-ready
	// Give the goroutine time to enter cond.Wait().
	time.Sleep(30 * time.Millisecond)

	// Release the first track; the goroutine should unblock.
	r1()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("blocked Track did not unblock after release")
	}
}

func TestCapped_TrackContextPreCancelled(t *testing.T) {
	t.Parallel()

	c := NewCapped[int](5)

	_, err := c.Track(context.Background(), 1, 5)
	if err != nil {
		t.Fatalf("first Track: %v", err)
	}

	// Pre-cancel the context before attempting a second Track.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = c.Track(ctx, 2, 5)
	if err == nil {
		t.Fatal("Track with pre-cancelled context must return error")
	}
}

func TestCapped_PendingAndHighest(t *testing.T) {
	t.Parallel()

	c := NewCapped[string](100)

	r1, _ := c.Track(context.Background(), "a", 20)
	r2, _ := c.Track(context.Background(), "b", 20)

	if p := c.Pending(); p != 40 {
		t.Fatalf("Pending: got %d, want 40", p)
	}
	if h := c.Highest(); h != nil {
		t.Fatal("Highest must be nil before any resolve")
	}

	r1()
	if h := c.Highest(); h == nil || *h != "a" {
		t.Fatalf("Highest after first resolve: got %v, want 'a'", h)
	}

	r2()
	if p := c.Pending(); p != 0 {
		t.Fatalf("Pending after all resolved: got %d, want 0", p)
	}
}

// ---------------------------------------------------------------------------
// Manager
// ---------------------------------------------------------------------------

func TestManager_TrackAndResolve(t *testing.T) {
	t.Parallel()

	m := NewManager[int](100, time.Hour)

	resolve, err := m.Track(context.Background(), 1)
	if err != nil {
		t.Fatalf("Track: %v", err)
	}

	highest, _ := resolve()
	if highest == nil || *highest != 1 {
		t.Fatalf("resolve: got %v, want 1", highest)
	}
}

func TestManager_ResolveReturnsHighest(t *testing.T) {
	t.Parallel()

	m := NewManager[int](100, time.Hour)

	r1, _ := m.Track(context.Background(), 10)
	r2, _ := m.Track(context.Background(), 20)
	r3, _ := m.Track(context.Background(), 30)

	r1()
	r2()
	highest, _ := r3()

	if highest == nil || *highest != 30 {
		t.Fatalf("Highest after all resolved: got %v, want 30", highest)
	}
}

func TestManager_ShouldCommitFalseWithinInterval(t *testing.T) {
	t.Parallel()

	m := NewManager[int](100, time.Hour)

	// The first resolve always commits because lastCommitNS starts at zero
	// (epoch), which is always older than commitEvery from now.
	r1, _ := m.Track(context.Background(), 1)
	_, firstCommit := r1()
	if !firstCommit {
		t.Fatal("first resolve must commit (lastCommitNS starts at epoch)")
	}

	// After the timer is reset, a second resolve within the 1-hour window
	// must NOT trigger a commit.
	r2, _ := m.Track(context.Background(), 2)
	_, shouldCommit := r2()
	if shouldCommit {
		t.Fatal("shouldCommit must be false when interval has not elapsed since last commit")
	}
}

func TestManager_ShouldCommitTrueAfterInterval(t *testing.T) {
	t.Parallel()

	m := NewManager[int](100, time.Nanosecond)
	time.Sleep(2 * time.Millisecond)

	resolve, _ := m.Track(context.Background(), 1)
	_, shouldCommit := resolve()
	if !shouldCommit {
		t.Fatal("shouldCommit must be true after interval elapsed")
	}
}

func TestManager_Reset(t *testing.T) {
	t.Parallel()

	m := NewManager[int](1, time.Hour)

	// Fill the single slot.
	_, _ = m.Track(context.Background(), 1)

	// Reset with larger capacity so subsequent tracks don't block.
	m.Reset(100)

	done := make(chan error, 1)
	go func() {
		_, err := m.Track(context.Background(), 2)
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Track after Reset: %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Track after Reset blocked unexpectedly")
	}
}

func TestManager_ContextPreCancelledWhileBlocked(t *testing.T) {
	t.Parallel()

	m := NewManager[int](1, time.Hour)

	_, err := m.Track(context.Background(), 1)
	if err != nil {
		t.Fatalf("first Track: %v", err)
	}

	// Pre-cancel context before attempting second Track against a full manager.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = m.Track(ctx, 2)
	if err == nil {
		t.Fatal("blocked Track must return error when context is pre-cancelled")
	}
}
