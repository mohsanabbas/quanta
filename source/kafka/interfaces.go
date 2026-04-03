package kafka

import (
	"context"

	"github.com/IBM/sarama"
)

type BackpressureManager interface {
	Acquire(ctx context.Context, size int64) error

	Release(size int64)

	Capacity() int64
}

type CheckpointManager interface {
	Track(offset int64, size int64) error

	Ack(offset int64) (AckHandle, int64, bool)

	Base() int64

	Initialized() bool

	Reset() []AckHandle

	Close()
}

type CommitStrategy interface {
	ShouldCommit(currentBase int64, newBase int64, pending uint32) bool

	MarkAndCommit(session sarama.ConsumerGroupSession, topic string, partition int32, offset int64)

	Flush(session sarama.ConsumerGroupSession, topic string, partition int32, offset int64)
}

type PartitionProcessor interface {
	ProcessMessage(sess sarama.ConsumerGroupSession, msg *sarama.ConsumerMessage, emit EmitFunc) error

	OnAck(handle AckHandle)

	Shutdown()
}
