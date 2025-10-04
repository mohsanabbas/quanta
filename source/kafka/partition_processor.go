package kafka

import (
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/IBM/sarama"
)

type partitionProcessor struct {
	driver          *SaramaDriver
	session         sarama.ConsumerGroupSession
	topic           string
	partition       int32
	backpressureMgr BackpressureManager
	checkpointMgr   CheckpointManager
	commitStrategy  CommitStrategy
	logger          *slog.Logger

	// periodic commit support
	stopCh chan struct{}
	wg     sync.WaitGroup
}

// newPartitionProcessor constructs a partition processor with injected dependencies.
func newPartitionProcessor(
	driver *SaramaDriver,
	sess sarama.ConsumerGroupSession,
	topic string,
	partition int32,
	backpressureMgr BackpressureManager,
	checkpointMgr CheckpointManager,
	commitStrategy CommitStrategy,
) *partitionProcessor {
	pp := &partitionProcessor{
		driver:          driver,
		session:         sess,
		topic:           topic,
		partition:       partition,
		backpressureMgr: backpressureMgr,
		checkpointMgr:   checkpointMgr,
		commitStrategy:  commitStrategy,
		logger:          driver.logger(slog.String("topic", topic), slog.Int("partition", int(partition))),
		stopCh:          make(chan struct{}),
	}

	// start a periodic commit checker
	pp.wg.Add(1)
	go pp.periodicCommitLoop()

	return pp
}

// periodicCommitLoop runs a background goroutine that checks if a periodic
// commit should be triggered based on time intervals. This ensures commits
// happen even when ACKS arrive slowly or out of order.
func (pp *partitionProcessor) periodicCommitLoop() {
	defer pp.wg.Done()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-pp.stopCh:
			return
		case <-ticker.C:
			if !pp.checkpointMgr.Initialized() {
				continue
			}

			base := pp.checkpointMgr.Base()
			if base < 0 {
				continue
			}

			//  check if we should commit based on time
			if pp.commitStrategy.ShouldCommit(base, base, 0) {
				pp.commitStrategy.MarkAndCommit(pp.session, pp.topic, pp.partition, base)
			}
		}
	}
}

// ProcessMessage handles a single Kafka message. It enforces backpressure
// constraints, converts the Sarama message into a pipeline Frame, and
// emits it via the provided emit function. In CommitAuto mode the offset
// is marked immediately after emission. In CommitE2E mode the message
// offset and size are tracked until an ack is received.
func (pp *partitionProcessor) ProcessMessage(sess sarama.ConsumerGroupSession, msg *sarama.ConsumerMessage, emit EmitFunc) error {
	size := estimateMessageSize(msg)

	// auto commit mode: acquire backpressure tokens, emit the frame, mark
	// the message and release tokens immediately.
	if pp.driver.mode == CommitAuto {
		if err := pp.backpressureMgr.Acquire(sess.Context(), size); err != nil {
			return err
		}
		frame := messageToFrame(msg)
		if err := emit(sess.Context(), frame); err != nil {
			pp.backpressureMgr.Release(size)
			return err
		}
		sess.MarkMessage(msg, "")
		pp.backpressureMgr.Release(size)
		return nil
	}

	// e2e commit mode: wait for a checkpoint slot and acquire backpressure.
	if err := pp.backpressureMgr.Acquire(sess.Context(), size); err != nil {
		return err
	}

	// track the message in the checkpoint manager
	if err := pp.checkpointMgr.Track(msg.Offset, size); err != nil {
		pp.backpressureMgr.Release(size)
		if errors.Is(err, ErrCheckpointClosed) {
			if ctxErr := sess.Context().Err(); ctxErr != nil {
				return ctxErr
			}
			return ErrCheckpointClosed
		}
		return err
	}

	frame := messageToFrame(msg)
	if err := emit(sess.Context(), frame); err != nil {
		// If emit fails, we need to clean up by removing from checkpoint manager
		// and releasing backpressure tokens. We must remove the tracked offset
		// to prevent double-release during shutdown.
		handle, _, _ := pp.checkpointMgr.Ack(msg.Offset)
		if handle.bytes > 0 {
			pp.backpressureMgr.Release(handle.bytes)
		} else {
			pp.backpressureMgr.Release(size)
		}
		return err
	}

	return nil
}

// OnAck processes an acknowledgment for a specific offset. It retrieves
// the handle from the checkpoint manager, releases backpressure tokens,
// and determines if the base offset has advanced to trigger a commit.
func (pp *partitionProcessor) OnAck(offsetHandle AckHandle) {
	currentBase := pp.checkpointMgr.Base()
	handle, newBase, advanced := pp.checkpointMgr.Ack(offsetHandle.offset)
	if handle.bytes == 0 {
		// Fallback to bytes provided by the caller if available.
		handle = offsetHandle
	}

	if handle.bytes == 0 {
		pp.logger.Debug("ack for unknown offset",
			slog.Int64("offset", offsetHandle.offset))
		return
	}

	pp.backpressureMgr.Release(handle.bytes)

	if pp.commitStrategy != nil {
		if advanced {
			if pp.commitStrategy.ShouldCommit(currentBase, newBase, 0) {
				pp.commitStrategy.MarkAndCommit(pp.session, pp.topic, pp.partition, newBase)
			}
		} else if newBase >= 0 {
			if pp.commitStrategy.ShouldCommit(newBase, newBase, 0) {
				pp.commitStrategy.MarkAndCommit(pp.session, pp.topic, pp.partition, newBase)
			}
		}
	}
}

// Shutdown performs cleanup when the partition is revoked. It stops the
// periodic commit loop and flushes the final commit. Note: we do NOT
// release backpressure tokens here because they are released when ACKS
// arrive or when emit fails. Reset() is called to clean up the checkpoint state.
func (pp *partitionProcessor) Shutdown() {
	// Stop the periodic commit loop
	close(pp.stopCh)
	pp.wg.Wait()

	// Reset checkpoint manager to clean up state
	// Note: Do NOT release backpressure tokens here to avoid double-release
	// Tokens are released when:
	// 1. Acks arrive (OnAck)
	// 2. Emit fails (ProcessMessage error path)
	// 3. Context cancelled (consumer group cleanup)
	_ = pp.checkpointMgr.Reset()

	// Flush the final commit if we have a valid base
	if pp.checkpointMgr.Initialized() {
		base := pp.checkpointMgr.Base()
		if base >= 0 && pp.commitStrategy != nil {
			pp.commitStrategy.MarkAndCommit(pp.session, pp.topic, pp.partition, base)
		}
	}
}
