package kafka

import (
	"log/slog"
	"sync"
	"time"

	"github.com/IBM/sarama"
)

type AckBasedCommitStrategy struct {
	step       int64
	lastCommit int64
	pending    uint32
	mu         sync.Mutex
	logger     *slog.Logger
}

func NewAckBasedCommitStrategy(step int64, logger *slog.Logger) *AckBasedCommitStrategy {
	if step <= 0 {
		step = 1
	}
	return &AckBasedCommitStrategy{
		step:       step,
		lastCommit: -1,
		logger:     logger,
	}
}

func (s *AckBasedCommitStrategy) ShouldCommit(
	currentBase int64,
	newBase int64,
	pending uint32,
) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.lastCommit < 0 {
		s.lastCommit = currentBase
	}

	s.pending = pending
	return (newBase - s.lastCommit) >= s.step
}

func (s *AckBasedCommitStrategy) MarkAndCommit(
	session sarama.ConsumerGroupSession,
	topic string,
	partition int32,
	offset int64,
) {
	s.mu.Lock()
	defer s.mu.Unlock()

	session.MarkOffset(topic, partition, offset, "")
	session.Commit()
	s.lastCommit = offset
	s.logger.Debug("ack-based commit",
		slog.String("topic", topic),
		slog.Int("partition", int(partition)),
		slog.Int64("offset", offset))
}

func (s *AckBasedCommitStrategy) Flush(
	session sarama.ConsumerGroupSession,
	topic string,
	partition int32,
	offset int64,
) {
	s.mu.Lock()
	defer s.mu.Unlock()

	session.MarkOffset(topic, partition, offset, "")
	session.Commit()
	s.lastCommit = offset
	s.logger.Info("flushed commit on shutdown",
		slog.String("topic", topic),
		slog.Int("partition", int(partition)),
		slog.Int64("offset", offset))
}

type PeriodicCommitStrategy struct {
	interval   time.Duration
	lastCommit time.Time
	mu         sync.Mutex
	logger     *slog.Logger
}

func NewPeriodicCommitStrategy(
	interval time.Duration,
	logger *slog.Logger,
) *PeriodicCommitStrategy {
	if interval <= 0 {
		interval = _defaultCommitInterval
	}
	return &PeriodicCommitStrategy{
		interval:   interval,
		lastCommit: time.Now(),
		logger:     logger,
	}
}

func (s *PeriodicCommitStrategy) ShouldCommit(_ int64, _ int64, _ uint32) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return time.Since(s.lastCommit) >= s.interval
}

func (s *PeriodicCommitStrategy) MarkAndCommit(
	session sarama.ConsumerGroupSession,
	topic string,
	partition int32,
	offset int64,
) {
	s.mu.Lock()
	defer s.mu.Unlock()

	session.MarkOffset(topic, partition, offset, "")
	session.Commit()
	s.lastCommit = time.Now()
	s.logger.Debug("periodic commit",
		slog.String("topic", topic),
		slog.Int("partition", int(partition)),
		slog.Int64("offset", offset))
}

func (s *PeriodicCommitStrategy) Flush(
	session sarama.ConsumerGroupSession,
	topic string,
	partition int32,
	offset int64,
) {
	s.mu.Lock()
	defer s.mu.Unlock()

	session.MarkOffset(topic, partition, offset, "")
	session.Commit()
	s.lastCommit = time.Now()
	s.logger.Info("flushed commit on shutdown",
		slog.String("topic", topic),
		slog.Int("partition", int(partition)),
		slog.Int64("offset", offset))
}

type HybridCommitStrategy struct {
	step       int64
	interval   time.Duration
	lastCommit int64
	lastTime   time.Time
	mu         sync.Mutex
	logger     *slog.Logger
}

func NewHybridCommitStrategy(
	step int64,
	interval time.Duration,
	logger *slog.Logger,
) *HybridCommitStrategy {
	if step <= 0 {
		step = _defaultCommitStep
	}
	if interval <= 0 {
		interval = _defaultCommitInterval
	}
	return &HybridCommitStrategy{
		step:       step,
		interval:   interval,
		lastCommit: -1,
		lastTime:   time.Now(),
		logger:     logger,
	}
}

func (s *HybridCommitStrategy) ShouldCommit(
	currentBase, newBase int64,
	_ uint32,
) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.lastCommit < 0 {
		s.lastCommit = currentBase
	}

	stepReached := (newBase - s.lastCommit) >= s.step
	timeReached := time.Since(s.lastTime) >= s.interval

	return stepReached || timeReached
}

func (s *HybridCommitStrategy) MarkAndCommit(
	session sarama.ConsumerGroupSession,
	topic string,
	partition int32,
	offset int64,
) {
	s.mu.Lock()
	defer s.mu.Unlock()

	session.MarkOffset(topic, partition, offset, "")
	session.Commit()
	s.lastCommit = offset
	s.lastTime = time.Now()
	s.logger.Debug("hybrid commit",
		slog.String("topic", topic),
		slog.Int("partition", int(partition)),
		slog.Int64("offset", offset))
}

func (s *HybridCommitStrategy) Flush(
	session sarama.ConsumerGroupSession,
	topic string,
	partition int32,
	offset int64,
) {
	s.mu.Lock()
	defer s.mu.Unlock()

	session.MarkOffset(topic, partition, offset, "")
	session.Commit()
	s.lastCommit = offset
	s.lastTime = time.Now()
	s.logger.Info("flushed commit on shutdown",
		slog.String("topic", topic),
		slog.Int("partition", int(partition)),
		slog.Int64("offset", offset))
}
