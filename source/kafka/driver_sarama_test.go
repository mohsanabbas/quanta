package kafka

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	pb "quanta/api/proto/v1"
)

func makeKafkaToken(topic string, part int32, off int64) *pb.CheckpointToken {
	return &pb.CheckpointToken{Kind: &pb.CheckpointToken_Kafka{Kafka: &pb.KafkaOffset{Topic: topic, Partition: part, Offset: off}}}
}

func TestSaramaDriver_OnAckDispatchesCallback(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	d := &SaramaDriver{}
	d.ackManager = newAckTracker[recordID]()
	if err := d.ackManager.Start(ctx); err != nil {
		t.Fatalf("start ack tracker: %v", err)
	}

	var called atomic.Int32
	rec := recordID{"t", 2, 99}
	d.ackManager.Track(rec, func() {
		called.Add(1)
	})

	tok := makeKafkaToken(rec.topic, rec.partition, rec.offset)
	d.OnAck(&pb.ConnectorAck{Checkpoint: tok})

	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	for {
		if called.Load() == 1 {
			return
		}
		select {
		case <-deadline.C:
			t.Fatal("callback not invoked")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func TestAckTrackerResetDropsCallbacks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tracker := newAckTracker[recordID]()
	if err := tracker.Start(ctx); err != nil {
		t.Fatalf("start ack tracker: %v", err)
	}

	var called atomic.Int32
	rec := recordID{"t", 1, 42}
	tracker.Track(rec, func() { called.Add(1) })

	if dropped := tracker.Reset(); dropped != 1 {
		t.Fatalf("expected 1 dropped callback, got %d", dropped)
	}

	tracker.Ack(rec)
	// Brief wait to ensure callback would have fired if still registered.
	time.Sleep(50 * time.Millisecond)
	if called.Load() != 0 {
		t.Fatal("callback should have been cleared by reset")
	}
}
