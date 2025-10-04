package kafka

import (
	"context"
	
	"github.com/IBM/sarama"
)

// BackpressureManager applies backpressure based on configurable rules
// (e.g., message count, byte size).
type BackpressureManager interface {
	// Acquire gets backpressure tokens for processing a message.
	// It blocks until tokens are available or the context is canceled.
	Acquire(ctx context.Context, size int64) error
	
	// Release returns previously acquired backpressure tokens.
	Release(size int64)
	
	// Capacity returns the total capacity of the backpressure manager.
	Capacity() int64
}

// CheckpointManager tracks processed offsets and determines the next offset
// to commit based.
type CheckpointManager interface {
	// Track registers an offset for tracking. This may apply backpressure
	// if the tracking window is full.
	Track(offset int64, size int64) error
	
	// Ack acknowledges that an offset has been fully processed. The returned
	// AckHandle carries the original bookkeeping information (including the
	// message size) so callers can release backpressure tokens even when the
	// acknowledgment arrives out of a band. The second return value contains the
	// next offset to commit, and the third reports whether the base advanced.
	Ack(offset int64) (AckHandle, int64, bool)
	
	// Base returns the current base offset (earliest unacknowledged).
	Base() int64
	
	// Initialized returns whether the checkpoint manager has been initialized.
	Initialized() bool
	
	// Reset drains all pending entries and returns handles for cleanup.
	Reset() []AckHandle
	
	// Close releases resources held by the checkpoint manager.
	Close()
}

// CommitStrategy determines when to commit offsets based on various conditions
// such as time intervals, message counts, or acknowledgment patterns.
type CommitStrategy interface {
	// ShouldCommit determines whether a commit should be triggered based on
	// the current state.
	ShouldCommit(currentBase int64, newBase int64, pending uint32) bool
	
	// MarkAndCommit performs the actual commit operation and updates internal state.
	MarkAndCommit(session sarama.ConsumerGroupSession, topic string, partition int32, offset int64)
	
	// Flush forces a commit of the current offset, typically during shutdown.
	Flush(session sarama.ConsumerGroupSession, topic string, partition int32, offset int64)
}

// PartitionProcessor handles message processing for a single partition.
// It coordinates backpressure, checkpointing, and committing.
type PartitionProcessor interface {
	// ProcessMessage handles a single Kafka message from the partition.
	ProcessMessage(sess sarama.ConsumerGroupSession, msg *sarama.ConsumerMessage, emit EmitFunc) error
	
	// OnAck processes an acknowledgment for a specific offset.
	OnAck(handle AckHandle)
	
	// Shutdown performs cleanup when the partition is revoked.
	Shutdown()
}
