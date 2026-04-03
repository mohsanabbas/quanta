package kafka

import "testing"

func TestPartitionTrackerAdvance(t *testing.T) {
	tracker := NewPartitionTracker(8)
	tracker.Reset(100)

	tracker.Reserve(100)
	tracker.Reserve(101)
	tracker.Reserve(102)

	if _, advanced := tracker.AckOffset(101); advanced {
		t.Fatalf("ack out of order should not advance base")
	}

	if base, advanced := tracker.AckOffset(100); !advanced || base != 102 {
		t.Fatalf("expected base 102 after acking offset 100, got base=%d advanced=%v", base, advanced)
	}

	if base, advanced := tracker.AckOffset(102); !advanced || base != 103 {
		t.Fatalf("expected base 103 after acking remaining slots, got base=%d advanced=%v", base, advanced)
	}
}

func TestPartitionTrackerOverflow(t *testing.T) {
	tracker := NewPartitionTracker(4)
	tracker.Reset(10)
	for i := int64(10); i < 14; i++ {
		if slot := tracker.Reserve(i); slot == InvalidSlot {
			t.Fatalf("unexpected overflow for offset %d", i)
		}
	}
	if slot := tracker.Reserve(14); slot != InvalidSlot {
		t.Fatalf("expected overflow sentinel, got %d", slot)
	}
	if base := tracker.Base(); base != 10 {
		t.Fatalf("base should remain unchanged on overflow, got %d", base)
	}
}

func TestPartitionTrackerAckOutOfWindow(t *testing.T) {
	tracker := NewPartitionTracker(4)
	tracker.Reset(50)
	tracker.Reserve(50)
	if base, advanced := tracker.AckOffset(80); advanced || base != 50 {
		t.Fatalf("out-of-window ack should be ignored, base=%d advanced=%v", base, advanced)
	}
}

func TestAckerTrackAck(t *testing.T) {
	ackr := NewOffsetTracker(4)
	handle := AckHandle{offset: 10, bytes: 42}
	ackr.Track(10, handle)

	h, ok := ackr.Ack(10)
	if !ok {
		t.Fatalf("expected ack handle")
	}
	if h.offset != handle.offset || h.bytes != handle.bytes {
		t.Fatalf("unexpected handle values: got %+v want %+v", h, handle)
	}

	if _, ok := ackr.Ack(10); ok {
		t.Fatalf("ack should have been removed after first Ack")
	}
}
