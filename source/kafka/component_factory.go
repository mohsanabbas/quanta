package kafka

import (
	"errors"
	"log/slog"

	qerr "quanta/internal/errors"
)

func NewBackpressureManager(cfg Config) (BackpressureManager, error) {
	pub := cfg.Public()
	tun := cfg.Tuning()
	strategy := pub.BackpressureStrategy
	if strategy.IsZero() {
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
	strategy := pub.CheckpointStrategy
	if strategy.IsZero() {
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
	strategy := pub.CommitStrategyType
	if strategy.IsZero() {
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
