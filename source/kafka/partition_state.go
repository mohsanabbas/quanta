package kafka

import (
	"log/slog"
	"sync"
	"time"

	"github.com/IBM/sarama"
)

// backoff defines how long to sleep when waiting for an available slot in the
// sliding window. It prevents a tight spin loop when the in‑flight window
// is full. Adjust as necessary; it is kept small to avoid impacting latency.
const backoff = 200 * time.Microsecond

// partitionState encapsulates per‑partition state for the Sarama driver. A
// partitionState is created when a partition is assigned and is responsible
// for tracking in‑flight messages, advancing the commit window and releasing
// backpressure tokens when acks arrive. It is single‑owner: only the
// partition goroutine mutates its fields, so no additional locking is
// required outside of the contained mutexes.
type partitionState struct {
	driver    *SaramaDriver
	session   sarama.ConsumerGroupSession
	tracker   *PartitionTracker
	acker     *Acker
	topic     string
	partition int32
	tuning    Tuning
	logger    *slog.Logger

	mu          sync.Mutex
	pending     uint32
	lastCommit  time.Time
	initialized bool
}

// newPartitionState constructs a partitionState for the given topic and
// partition. It copies the tuning parameters to avoid concurrent mutation
// issues and initializes internal structures like the PartitionTracker and
// Acker. The partitionState does not begin consuming until processMessage
// is invoked by the group handler.
func newPartitionState(driver *SaramaDriver, sess sarama.ConsumerGroupSession, topic string, partition int32) *partitionState {
	tuning := driver.tuning
	if tuning.CommitInterval <= 0 {
		tuning.CommitInterval = 5 * time.Second
	}
	if tuning.CommitStep == 0 {
		tuning.CommitStep = 1
	}
	ps := &partitionState{
		driver:    driver,
		session:   sess,
		tracker:   NewPartitionTracker(tuning.WindowBits),
		acker:     NewAcker(int(tuning.InFlightMsgs)),
		topic:     topic,
		partition: partition,
		tuning:    tuning,
		logger:    driver.logger(slog.String("topic", topic), slog.Int("partition", int(partition))),
	}
	return ps
}

// processMessage handles a single Kafka message. It enforces backpressure
// constraints, converts the Sarama message into a pipeline Frame and
// emits it via the provided emit function. In CommitAuto mode the offset
// is marked immediately after emission. In CommitE2E mode the message
// offset and size are tracked until an ack is received.
func (ps *partitionState) processMessage(sess sarama.ConsumerGroupSession, msg *sarama.ConsumerMessage, emit EmitFunc) error {
	size := estimateMessageSize(msg)

	// Auto commit mode: acquire backpressure tokens, emit the frame, mark
	// the message and release tokens immediately.
	if ps.driver.mode == CommitAuto {
		if err := ps.driver.acquire(sess.Context(), size); err != nil {
			return err
		}
		frame := messageToFrame(msg)
		if err := emit(sess.Context(), frame); err != nil {
			ps.driver.release(size)
			return err
		}
		sess.MarkMessage(msg, "")
		ps.driver.release(size)
		return nil
	}

	// End‑to‑end commit mode: wait for an available slot in the ack window
	// before acquiring backpressure tokens. This ensures we never exceed
	// the configured window size.
	ps.waitForWindow(msg.Offset)
	if err := ps.driver.acquire(sess.Context(), size); err != nil {
		return err
	}
	ps.acker.Track(msg.Offset, ackHandle{offset: msg.Offset, bytes: size})

	frame := messageToFrame(msg)
	if err := emit(sess.Context(), frame); err != nil {
		// If the emit fails we must remove the pending record and release
		// backpressure tokens. Attempt to remove the handle from the acker.
		if h, ok := ps.acker.Remove(msg.Offset); ok {
			ps.driver.release(h.bytes)
		} else {
			ps.driver.release(size)
		}
		return err
	}
	return nil
}

// handleAck processes an ack for a single record. It advances the commit
// window and releases backpressure tokens according to the ack handle.
func (ps *partitionState) handleAck(handle ackHandle) {
	newBase, advanced := ps.tracker.AckOffset(handle.offset)
	ps.driver.release(handle.bytes)
	if advanced {
		ps.commit(newBase)
	}
}

// commit marks the current base offset in the Sarama session and flushes
// commits based on either the configured commit step or interval. It is
// safe to call concurrently with processMessage; internal state is
// protected by a mutex.
func (ps *partitionState) commit(newBase int64) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	offset := newBase
	ps.session.MarkOffset(ps.topic, ps.partition, offset, "")
	ps.pending++
	// Flush on reaching the commit step or commit interval.
	if ps.pending >= ps.tuning.CommitStep || time.Since(ps.lastCommit) >= ps.tuning.CommitInterval {
		ps.session.Commit()
		ps.logger.Debug("commit flushed", slog.Int64("offset", offset))
		ps.pending = 0
		ps.lastCommit = time.Now()
	}
}

// flush commits the current base offset immediately. It is used when
// shutting down a partition or when a partition is revoked. It resets
// pending counters and updates the last commit timestamp.
func (ps *partitionState) flush() {
	// Skip if the tracker has not been initialized or the base is negative.
	if !ps.tracker.Initialized() {
		return
	}
	base := ps.tracker.Base()
	if base < 0 {
		return
	}
	ps.mu.Lock()
	defer ps.mu.Unlock()
	offset := base
	ps.session.MarkOffset(ps.topic, ps.partition, offset, "")
	ps.session.Commit()
	ps.logger.Info("commit on shutdown", slog.Int64("offset", offset))
	ps.pending = 0
	ps.lastCommit = time.Now()
}

// shutdown drains all in‑flight acknowledgments, releases any remaining
// backpressure tokens and flushes the commit offset. It should be called
// exactly once when a partition is revoked or when the consumer is
// shutting down.
func (ps *partitionState) shutdown() {
	// Drain outstanding handles and release their tokens.
	handles := ps.acker.Reset()
	for _, h := range handles {
		ps.driver.release(h.bytes)
	}
	// Flush the commit offset.
	ps.flush()
	// Close the acker to prevent further tracking.
	ps.acker.Close()
}

// waitForWindow blocks until Reserve returns a valid slot for the given
// offset. It sleeps for a short duration to avoid a tight spin loop. This
// method enforces the invariant that the number of in‑flight messages never
// exceeds the configured window size.
func (ps *partitionState) waitForWindow(offset int64) {
	for {
		if ps.tracker.Reserve(offset) != InvalidSlot {
			return
		}
		time.Sleep(backoff)
	}
}
