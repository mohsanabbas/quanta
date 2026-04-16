package checkpoint

import (
	"context"
	"testing"
	"time"
)

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

	tests := []struct {
		name    string
		payloads []int
		sizes    []int64
		wantSeq  []int
	}{
		{
			name:     "three_in_order",
			payloads: []int{10, 20, 30},
			sizes:    []int64{5, 5, 5},
			wantSeq:  []int{10, 20, 30},
		},
		{
			name:     "single_element",
			payloads: []int{42},
			sizes:    []int64{8},
			wantSeq:  []int{42},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			u := NewUncapped[int]()
			resolvers := make([]func() *int, len(tt.payloads))
			for i, p := range tt.payloads {
				resolvers[i] = u.Track(p, tt.sizes[i])
			}
			for i, r := range resolvers {
				h := r()
				if h == nil || *h != tt.wantSeq[i] {
					t.Fatalf("resolve[%d]: got %v, want %d", i, h, tt.wantSeq[i])
				}
			}
			if p := u.Pending(); p != 0 {
				t.Fatalf("Pending after all resolved: got %d, want 0", p)
			}
		})
	}
}

func TestUncapped_OutOfOrderResolveHeadLast(t *testing.T) {
	t.Parallel()

	u := NewUncapped[int]()
	r1 := u.Track(1, 10)
	r2 := u.Track(2, 10)
	r3 := u.Track(3, 10)

	r3()
	r2()
	if h := u.Highest(); h != nil {
		t.Fatalf("Highest before head resolved: got %v, want nil", h)
	}

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

	h := r1()
	if h == nil || *h != 1 {
		t.Fatalf("after head resolve: got %v, want 1", h)
	}
	if p := u.Pending(); p != 10 {
		t.Fatalf("Pending after head resolve: got %d, want 10", p)
	}
}

func TestUncapped_PendingSumsSizes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		sizes   []int64
		wantSum int64
	}{
		{name: "three_equal", sizes: []int64{10, 20, 30}, wantSum: 60},
		{name: "single", sizes: []int64{7}, wantSum: 7},
		{name: "two_items", sizes: []int64{100, 200}, wantSum: 300},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			u := NewUncapped[string]()
			for i, s := range tt.sizes {
				u.Track("v", s)
				_ = i
			}
			if p := u.Pending(); p != tt.wantSum {
				t.Fatalf("Pending: got %d, want %d", p, tt.wantSum)
			}
		})
	}
}

func TestCapped_TrackWithinCapacityDoesNotBlock(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		cap      int64
		batch    int64
		trackN   int
	}{
		{name: "three_within_100", cap: 100, batch: 30, trackN: 3},
		{name: "one_exactly_at_cap", cap: 50, batch: 50, trackN: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := NewCapped[int](tt.cap)
			for i := 0; i < tt.trackN; i++ {
				done := make(chan error, 1)
				go func(idx int) {
					_, err := c.Track(context.Background(), idx, tt.batch)
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
		})
	}
}

func TestCapped_TrackBlocksWhenCapacityExceeded(t *testing.T) {
	t.Parallel()

	c := NewCapped[int](10)

	r1, err := c.Track(context.Background(), 1, 10)
	if err != nil {
		t.Fatalf("first Track: %v", err)
	}

	type result struct {
		resolve func() *int
		err     error
	}
	resultCh := make(chan result, 1)
	started := make(chan struct{})

	c.cond.L.Lock()
	go func() {
		close(started)
		resolve, err := c.Track(context.Background(), 2, 10)
		resultCh <- result{resolve: resolve, err: err}
	}()

	<-started
	select {
	case res := <-resultCh:
		if res.resolve != nil {
			res.resolve()
		}
		c.cond.L.Unlock()
		t.Fatal("Track returned before waiting for capacity")
	default:
	}
	c.cond.L.Unlock()

	c.cond.L.Lock()
	select {
	case res := <-resultCh:
		if res.resolve != nil {
			res.resolve()
		}
		c.cond.L.Unlock()
		t.Fatal("Track returned before release")
	default:
	}
	c.cond.L.Unlock()

	r1()

	select {
	case res := <-resultCh:
		if res.err != nil {
			t.Fatalf("blocked Track: %v", res.err)
		}
		if res.resolve == nil {
			t.Fatal("blocked Track returned nil resolve")
		}
		res.resolve()
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

	tests := []struct {
		name     string
		payloads []int
		want     int
	}{
		{name: "three_sequential", payloads: []int{10, 20, 30}, want: 30},
		{name: "single", payloads: []int{42}, want: 42},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := NewManager[int](100, time.Hour)
			resolvers := make([]func() (*int, bool), len(tt.payloads))
			for i, p := range tt.payloads {
				r, err := m.Track(context.Background(), p)
				if err != nil {
					t.Fatalf("Track[%d]: %v", i, err)
				}
				resolvers[i] = r
			}
			var last *int
			for _, r := range resolvers {
				h, _ := r()
				last = h
			}
			if last == nil || *last != tt.want {
				t.Fatalf("Highest after all resolved: got %v, want %d", last, tt.want)
			}
		})
	}
}

func TestManager_ShouldCommitFalseWithinInterval(t *testing.T) {
	t.Parallel()

	m := NewManager[int](100, time.Hour)

	r1, _ := m.Track(context.Background(), 1)
	_, firstCommit := r1()
	if !firstCommit {
		t.Fatal("first resolve must commit (lastCommitNS starts at epoch)")
	}

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

	_, _ = m.Track(context.Background(), 1)

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

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = m.Track(ctx, 2)
	if err == nil {
		t.Fatal("blocked Track must return error when context is pre-cancelled")
	}
}
