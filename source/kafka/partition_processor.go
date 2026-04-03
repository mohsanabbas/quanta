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

	stopCh chan struct{}
	wg     sync.WaitGroup
}

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

	pp.wg.Add(1)
	go pp.periodicCommitLoop()

	return pp
}

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

			if pp.commitStrategy.ShouldCommit(base, base, 0) {
				pp.commitStrategy.MarkAndCommit(pp.session, pp.topic, pp.partition, base)
			}
		}
	}
}

func (pp *partitionProcessor) ProcessMessage(sess sarama.ConsumerGroupSession, msg *sarama.ConsumerMessage, emit EmitFunc) error {
	size := estimateMessageSize(msg)

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

	if err := pp.backpressureMgr.Acquire(sess.Context(), size); err != nil {
		return err
	}

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

func (pp *partitionProcessor) OnAck(offsetHandle AckHandle) {
	currentBase := pp.checkpointMgr.Base()
	handle, newBase, advanced := pp.checkpointMgr.Ack(offsetHandle.offset)
	if handle.bytes == 0 {
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

func (pp *partitionProcessor) Shutdown() {
	close(pp.stopCh)
	pp.wg.Wait()

	_ = pp.checkpointMgr.Reset()

	if pp.checkpointMgr.Initialized() {
		base := pp.checkpointMgr.Base()
		if base >= 0 && pp.commitStrategy != nil {
			pp.commitStrategy.MarkAndCommit(pp.session, pp.topic, pp.partition, base)
		}
	}
}
