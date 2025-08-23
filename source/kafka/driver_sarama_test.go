package kafka

import (
	"sync/atomic"
	"testing"

	pb "quanta/api/proto/v1"
)

func makeKafkaToken(topic string, part int32, off int64) *pb.CheckpointToken {
	return &pb.CheckpointToken{Kind: &pb.CheckpointToken_Kafka{Kafka: &pb.KafkaOffset{Topic: topic, Partition: part, Offset: off}}}
}

func TestSaramaDriver_OnAck_Enqueue(t *testing.T) {
	d := &SaramaDriver{}
	d.ackCh = make(chan recordID, 1)

	tok := makeKafkaToken("t", 1, 42)
	d.OnAck(&pb.ConnectorAck{Checkpoint: tok})

	rec := <-d.ackCh
	if rec.topic != "t" || rec.partition != 1 || rec.offset != 42 {
		t.Fatalf("unexpected record enqueued: %+v", rec)
	}
}

func TestSaramaDriver_AckCallbackProcessed(t *testing.T) {
	d := &SaramaDriver{}
	d.ackCh = make(chan recordID, 1)
	d.pending = make(map[recordID]func())

	var called int32
	rec := recordID{"t", 2, 99}
	d.pending[rec] = func() { atomic.AddInt32(&called, 1) }

	tok := makeKafkaToken(rec.topic, rec.partition, rec.offset)
	d.OnAck(&pb.ConnectorAck{Checkpoint: tok})

	got := <-d.ackCh
	if got != rec {
		t.Fatalf("unexpected rec from ackCh: %+v", got)
	}
	d.mu.Lock()
	cb, ok := d.pending[got]
	if ok {
		delete(d.pending, got)
	}
	d.mu.Unlock()
	if !ok {
		t.Fatal("callback not found in pending map")
	}
	cb()
	if atomic.LoadInt32(&called) != 1 {
		t.Fatal("callback was not executed exactly once")
	}
}
