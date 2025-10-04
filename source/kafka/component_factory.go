package kafka

import (
	"fmt"
	"log/slog"
)

// BackpressureStrategy defines the type of backpressure to apply.
type BackpressureStrategy string

const (
	BackpressureStrategyCount    BackpressureStrategy = "count"
	BackpressureStrategySize     BackpressureStrategy = "size"
	BackpressureStrategyCombined BackpressureStrategy = "combined"
)

// CheckpointStrategy defines the checkpoint tracking approach.
type CheckpointStrategy string

const (
	CheckpointStrategySlidingWindow         CheckpointStrategy = "sliding_window"
	CheckpointStrategyApplicationControlled CheckpointStrategy = "application_controlled"
)

// CommitStrategyType defines when to commit offsets in e2e mode.
type CommitStrategyType string

const (
	CommitStrategyTypeAckBased CommitStrategyType = "ack_based"
	CommitStrategyTypePeriodic CommitStrategyType = "periodic"
	CommitStrategyTypeHybrid   CommitStrategyType = "hybrid"
)

// NewBackpressureManager creates a BackpressureManager based on the
// configured strategy and tuning parameters.
func NewBackpressureManager(cfg Config) (BackpressureManager, error) {
	pub := cfg.Public()
	tun := cfg.Tuning()
	strategy := BackpressureStrategy(pub.BackpressureStrategy)
	if strategy == "" {
		strategy = BackpressureStrategyCombined // default
	}

	switch strategy {
	case BackpressureStrategyCount:
		return NewCountBasedBackpressureManager(tun.InFlightMsgs), nil
	case BackpressureStrategySize:
		return NewSizeBasedBackpressureManager(tun.InFlightBytes), nil
	case BackpressureStrategyCombined:
		return NewCombinedBackpressureManager(tun.InFlightBytes, tun.InFlightMsgs), nil
	default:
		return nil, fmt.Errorf("unknown backpressure strategy: %s", strategy)
	}
}

// NewCheckpointManager creates a CheckpointManager based on the configured
// strategy and tuning parameters.
func NewCheckpointManager(cfg Config) (CheckpointManager, error) {
	pub := cfg.Public()
	tun := cfg.Tuning()
	strategy := CheckpointStrategy(pub.CheckpointStrategy)
	if strategy == "" {
		strategy = CheckpointStrategySlidingWindow // default
	}

	switch strategy {
	case CheckpointStrategySlidingWindow:
		return NewSlidingWindowCheckpointManager(tun.WindowBits, int(tun.InFlightMsgs)), nil
	case CheckpointStrategyApplicationControlled:
		return NewApplicationControlledCheckpointManager(tun.InFlightMsgs), nil
	default:
		return nil, fmt.Errorf("unknown checkpoint strategy: %s", strategy)
	}
}

// NewCommitStrategy creates a CommitStrategy based on the configured
// strategy and tuning parameters.
func NewCommitStrategy(cfg Config, logger *slog.Logger) (CommitStrategy, error) {
	pub := cfg.Public()
	tun := cfg.Tuning()
	strategy := CommitStrategyType(pub.CommitStrategyType)
	if strategy == "" {
		strategy = CommitStrategyTypeHybrid // default
	}

	switch strategy {
	case CommitStrategyTypeAckBased:
		return NewAckBasedCommitStrategy(int64(tun.CommitStep), logger), nil
	case CommitStrategyTypePeriodic:
		return NewPeriodicCommitStrategy(tun.CommitInterval, logger), nil
	case CommitStrategyTypeHybrid:
		return NewHybridCommitStrategy(int64(tun.CommitStep), tun.CommitInterval, logger), nil
	default:
		return nil, fmt.Errorf("unknown commit strategy: %s", strategy)
	}
}
