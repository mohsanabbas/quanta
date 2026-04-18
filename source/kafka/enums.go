// Package kafka — enum types as slug-struct enums.
//
// Pattern follows Three Dots Labs "Safer Enums in Go" doctrine
// (https://threedots.tech/post/safer-enums-in-go/):
//
//   - Each enum is a struct with a single unexported `slug` field. Because
//     the field is unexported, downstream packages cannot construct invalid
//     values from outside `package kafka`.
//   - The zero value (`T{}`) is the explicit `Unknown*` sentinel — used to
//     detect "not set" and trigger defaults.
//   - `FromString` is the named constructor that validates against the known
//     slugs, returning `Unknown*` + a domain-typed error on failure.
//   - `encoding.TextMarshaler` / `TextUnmarshaler` is the wire seam — yaml.v3
//     and koanf+mapstructure both honour it (koanf's default decoder config
//     already wires the text-unmarshaller hook).
//   - Errors flow through `qerr` (no `fmt.Errorf`); leaf strings come from
//     stdlib `errors.New`.
package kafka

import (
	"errors"
	"strconv"
	"strings"

	qerr "quanta/internal/errors"
)

// ---------------------------------------------------------------------------
// BackpressureStrategy
// ---------------------------------------------------------------------------

type BackpressureStrategy struct{ slug string }

var (
	UnknownBackpressureStrategy  = BackpressureStrategy{}
	BackpressureStrategyCount    = BackpressureStrategy{"count"}
	BackpressureStrategySize     = BackpressureStrategy{"size"}
	BackpressureStrategyCombined = BackpressureStrategy{"combined"}
)

func (v BackpressureStrategy) String() string { return v.slug }
func (v BackpressureStrategy) IsZero() bool   { return v.slug == "" }

func BackpressureStrategyFromString(s string) (BackpressureStrategy, error) {
	switch strings.ToLower(s) {
	case "":
		return UnknownBackpressureStrategy, nil
	case BackpressureStrategyCount.slug:
		return BackpressureStrategyCount, nil
	case BackpressureStrategySize.slug:
		return BackpressureStrategySize, nil
	case BackpressureStrategyCombined.slug:
		return BackpressureStrategyCombined, nil
	}
	return UnknownBackpressureStrategy, qerr.Config("kafka", "parse-backpressure-strategy",
		errors.New("unknown backpressure_strategy "+strconv.Quote(s)+" (want one of: count, size, combined)"))
}

func (v BackpressureStrategy) MarshalText() ([]byte, error) { return []byte(v.slug), nil }
func (v *BackpressureStrategy) UnmarshalText(text []byte) error {
	parsed, err := BackpressureStrategyFromString(string(text))
	if err != nil {
		return err
	}
	*v = parsed
	return nil
}

// ---------------------------------------------------------------------------
// CheckpointStrategy
// ---------------------------------------------------------------------------

type CheckpointStrategy struct{ slug string }

var (
	UnknownCheckpointStrategy               = CheckpointStrategy{}
	CheckpointStrategySlidingWindow         = CheckpointStrategy{"sliding_window"}
	CheckpointStrategyApplicationControlled = CheckpointStrategy{"application_controlled"}
)

func (v CheckpointStrategy) String() string { return v.slug }
func (v CheckpointStrategy) IsZero() bool   { return v.slug == "" }

func CheckpointStrategyFromString(s string) (CheckpointStrategy, error) {
	switch strings.ToLower(s) {
	case "":
		return UnknownCheckpointStrategy, nil
	case CheckpointStrategySlidingWindow.slug:
		return CheckpointStrategySlidingWindow, nil
	case CheckpointStrategyApplicationControlled.slug:
		return CheckpointStrategyApplicationControlled, nil
	}
	return UnknownCheckpointStrategy, qerr.Config("kafka", "parse-checkpoint-strategy",
		errors.New("unknown checkpoint_strategy "+strconv.Quote(s)+" (want one of: sliding_window, application_controlled)"))
}

func (v CheckpointStrategy) MarshalText() ([]byte, error) { return []byte(v.slug), nil }
func (v *CheckpointStrategy) UnmarshalText(text []byte) error {
	parsed, err := CheckpointStrategyFromString(string(text))
	if err != nil {
		return err
	}
	*v = parsed
	return nil
}

// ---------------------------------------------------------------------------
// CommitStrategyType
// ---------------------------------------------------------------------------

type CommitStrategyType struct{ slug string }

var (
	UnknownCommitStrategyType  = CommitStrategyType{}
	CommitStrategyTypeAckBased = CommitStrategyType{"ack_based"}
	CommitStrategyTypePeriodic = CommitStrategyType{"periodic"}
	CommitStrategyTypeHybrid   = CommitStrategyType{"hybrid"}
)

func (v CommitStrategyType) String() string { return v.slug }
func (v CommitStrategyType) IsZero() bool   { return v.slug == "" }

func CommitStrategyTypeFromString(s string) (CommitStrategyType, error) {
	switch strings.ToLower(s) {
	case "":
		return UnknownCommitStrategyType, nil
	case CommitStrategyTypeAckBased.slug:
		return CommitStrategyTypeAckBased, nil
	case CommitStrategyTypePeriodic.slug:
		return CommitStrategyTypePeriodic, nil
	case CommitStrategyTypeHybrid.slug:
		return CommitStrategyTypeHybrid, nil
	}
	return UnknownCommitStrategyType, qerr.Config("kafka", "parse-commit-strategy-type",
		errors.New("unknown commit_strategy_type "+strconv.Quote(s)+" (want one of: ack_based, periodic, hybrid)"))
}

func (v CommitStrategyType) MarshalText() ([]byte, error) { return []byte(v.slug), nil }
func (v *CommitStrategyType) UnmarshalText(text []byte) error {
	parsed, err := CommitStrategyTypeFromString(string(text))
	if err != nil {
		return err
	}
	*v = parsed
	return nil
}
