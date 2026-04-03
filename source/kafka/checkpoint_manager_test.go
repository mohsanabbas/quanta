package kafka

import (
	"testing"
	"time"
)

func TestSlidingWindowCheckpointManagerAckReturnsHandle(t *testing.T) {
	mgr := NewSlidingWindowCheckpointManager(8, 4)

	if err := mgr.Track(100, 5); err != nil {
		t.Fatalf("track failed: %v", err)
	}

	handle, newBase, advanced := mgr.Ack(100)
	if handle.offset != 100 {
		t.Fatalf("unexpected handle offset: got %d want 100", handle.offset)
	}
	if handle.bytes != 5 {
		t.Fatalf("unexpected handle bytes: got %d want 5", handle.bytes)
	}
	if !advanced || newBase != 101 {
		t.Fatalf("expected base 101 advanced=true got base=%d advanced=%v", newBase, advanced)
	}

	emptyHandle, base, adv := mgr.Ack(500)

	if emptyHandle != (AckHandle{}) {
		t.Fatalf("expected empty handle for unknown offset, got %+v", emptyHandle)
	}
	if adv {
		t.Fatalf("unexpected advance for unknown offset")
	}
	if base != mgr.Base() {
		t.Fatalf("base should be unchanged for unknown offset")
	}
}

func TestApplicationControlledCheckpointManagerBlocksUntilAck(t *testing.T) {
	mgr := NewApplicationControlledCheckpointManager(1)

	if err := mgr.Track(10, 7); err != nil {
		t.Fatalf("track failed: %v", err)
	}

	done := make(chan struct{})
	go func() {
		if err := mgr.Track(11, 9); err != nil {
			t.Errorf("unexpected track error: %v", err)
		}
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("second track should block until ack")
	case <-time.After(30 * time.Millisecond):
	}

	handle, base, advanced := mgr.Ack(10)
	if handle.offset != 10 || handle.bytes != 7 {
		t.Fatalf("unexpected handle returned: %+v", handle)
	}
	if !advanced || base != 11 {
		t.Fatalf("expected base 11 advanced=true got base=%d advanced=%v", base, advanced)
	}

	select {
	case <-done:
	case <-time.After(50 * time.Millisecond):
		t.Fatal("second track did not unblock after ack")
	}

	handle2, base2, advanced2 := mgr.Ack(11)
	if handle2.offset != 11 || handle2.bytes != 9 {
		t.Fatalf("unexpected handle from second ack: %+v", handle2)
	}
	if !advanced2 || base2 != 12 {
		t.Fatalf("expected base 12 advanced=true got base=%d advanced=%v", base2, advanced2)
	}

	handles := mgr.Reset()
	if len(handles) != 0 {
		t.Fatalf("expected reset to return no handles, got %d", len(handles))
	}

	mgr.Close()
}
