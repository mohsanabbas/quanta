package kafka

import (
	"errors"
	"log/slog"

	qerr "quanta/internal/errors"
)

type BackpressureStrategy string

const (
	BackpressureStrategyCount    BackpressureStrategy = "count"
	BackpressureStrategySize     BackpressureStrategy = "size"
	BackpressureStrategyCombined BackpressureStrategy = "combined"
)

type CheckpointStrategy string

const (
	CheckpointStrategySlidingWindow         CheckpointStrategy = "sliding_window"
	CheckpointStrategyApplicationControlled CheckpointStrategy = "application_controlled"
)

type CommitStrategyType string

const (
	CommitStrategyTypeAckBased CommitStrategyType = "ack_based"
	CommitStrategyTypePeriodic CommitStrategyType = "periodic"
	CommitStrategyTypeHybrid   CommitStrategyType = "hybrid"
)

func NewBackpressureManager(cfg Config) (BackpressureManager, error) {
	pub := cfg.Public()
	tun := cfg.Tuning()
	strategy := BackpressureStrategy(pub.BackpressureStrategy)
	if strategy == "" {
		strategy = BackpressureStrategyCombined
	}

	switch strategy {
	case BackpressureStrategyCount:
		return NewCountBasedBackpressureManager(tun.InFlightMsgs), nil
	case BackpressureStrategySize:
		return NewSizeBasedBackpressureManager(tun.InFlightBytes), nil
	case BackpressureStrategyCombined:
		return NewCombinedBackpressureManager(tun.InFlightBytes, tun.InFlightMsgs), nil
	default:
		return nil, qerr.Config("kafka", "backpressure", errors.New("unsupported backpressure strategy"))
	}
}

func NewCheckpointManager(cfg Config) (CheckpointManager, error) {
	pub := cfg.Public()
	tun := cfg.Tuning()
	strategy := CheckpointStrategy(pub.CheckpointStrategy)
	if strategy == "" {
		strategy = CheckpointStrategySlidingWindow
	}

	switch strategy {
	case CheckpointStrategySlidingWindow:
		return NewSlidingWindowCheckpointManager(tun.WindowBits, int(tun.InFlightMsgs)), nil
	case CheckpointStrategyApplicationControlled:
		return NewApplicationControlledCheckpointManager(tun.InFlightMsgs), nil
	default:
		return nil, qerr.Config("kafka", "checkpoint", errors.New("unsupported checkpoint strategy"))
	}
}

func NewCommitStrategy(cfg Config, logger *slog.Logger) (CommitStrategy, error) {
	pub := cfg.Public()
	tun := cfg.Tuning()
	strategy := CommitStrategyType(pub.CommitStrategyType)
	if strategy == "" {
		strategy = CommitStrategyTypeHybrid
	}

	switch strategy {
	case CommitStrategyTypeAckBased:
		return NewAckBasedCommitStrategy(int64(tun.CommitStep), logger), nil
	case CommitStrategyTypePeriodic:
		return NewPeriodicCommitStrategy(tun.CommitInterval, logger), nil
	case CommitStrategyTypeHybrid:
		return NewHybridCommitStrategy(int64(tun.CommitStep), tun.CommitInterval, logger), nil
	default:
		return nil, qerr.Config("kafka", "commit", errors.New("unsupported commit strategy"))
	}
}
