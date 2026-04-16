package kafka

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/IBM/sarama"
)

// mockSession records calls made by commit strategies for assertion.
type mockSession struct {
	marks   []mockMark
	commits int
}

type mockMark struct {
	topic     string
	partition int32
	offset    int64
}

func (m *mockSession) MarkOffset(topic string, partition int32, offset int64, _ string) {
	m.marks = append(m.marks, mockMark{topic: topic, partition: partition, offset: offset})
}

func (m *mockSession) Commit() { m.commits++ }

func (m *mockSession) Claims() map[string][]int32                       { return nil }
func (m *mockSession) MemberID() string                                  { return "" }
func (m *mockSession) GenerationID() int32                               { return 0 }
func (m *mockSession) ResetOffset(_ string, _ int32, _ int64, _ string) {}
func (m *mockSession) MarkMessage(_ *sarama.ConsumerMessage, _ string)  {}
func (m *mockSession) Context() context.Context                          { return context.Background() }

var _ sarama.ConsumerGroupSession = (*mockSession)(nil)

func nullLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(noopWriter{}, nil))
}

type noopWriter struct{}

func (noopWriter) Write(p []byte) (int, error) { return len(p), nil }

// ---------------------------------------------------------------------------
// AckBasedCommitStrategy
// ---------------------------------------------------------------------------

func TestAckBasedCommitStrategy_ShouldCommit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		step        int64
		currentBase int64
		newBase     int64
		want        bool
	}{
		{
			name:        "step_reached",
			step:        5,
			currentBase: 100,
			newBase:     105,
			want:        true,
		},
		{
			name:        "step_not_reached",
			step:        5,
			currentBase: 100,
			newBase:     104,
			want:        false,
		},
		{
			name:        "step_1_always_commits",
			step:        1,
			currentBase: 200,
			newBase:     201,
			want:        true,
		},
		{
			name:        "zero_step_normalized_to_1",
			step:        0,
			currentBase: 10,
			newBase:     11,
			want:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := NewAckBasedCommitStrategy(tt.step, nullLogger())
			got := s.ShouldCommit(tt.currentBase, tt.newBase, 0)
			if got != tt.want {
				t.Fatalf("ShouldCommit: got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAckBasedCommitStrategy_ShouldCommit_UpdatesAfterMarkAndCommit(t *testing.T) {
	t.Parallel()

	s := NewAckBasedCommitStrategy(5, nullLogger())
	sess := &mockSession{}

	// First window: 100 → 105 commits.
	if !s.ShouldCommit(100, 105, 0) {
		t.Fatal("expected ShouldCommit=true at step boundary")
	}
	s.MarkAndCommit(sess, "t", 0, 105)

	// Within next step: 105 → 109 should not commit.
	if s.ShouldCommit(105, 109, 0) {
		t.Fatal("expected ShouldCommit=false before next step boundary")
	}

	// At next step boundary: 105 → 110 should commit.
	if !s.ShouldCommit(105, 110, 0) {
		t.Fatal("expected ShouldCommit=true at next step boundary")
	}
}

func TestAckBasedCommitStrategy_MarkAndCommit(t *testing.T) {
	t.Parallel()

	s := NewAckBasedCommitStrategy(1, nullLogger())
	sess := &mockSession{}

	s.MarkAndCommit(sess, "orders", 3, 42)

	if len(sess.marks) != 1 {
		t.Fatalf("MarkOffset calls: got %d, want 1", len(sess.marks))
	}
	if sess.marks[0].topic != "orders" || sess.marks[0].partition != 3 || sess.marks[0].offset != 42 {
		t.Fatalf("MarkOffset args: got %+v", sess.marks[0])
	}
	if sess.commits != 1 {
		t.Fatalf("Commit calls: got %d, want 1", sess.commits)
	}
}

func TestAckBasedCommitStrategy_Flush(t *testing.T) {
	t.Parallel()

	s := NewAckBasedCommitStrategy(1, nullLogger())
	sess := &mockSession{}

	s.Flush(sess, "events", 1, 99)

	if len(sess.marks) != 1 {
		t.Fatalf("MarkOffset calls: got %d, want 1", len(sess.marks))
	}
	if sess.marks[0].offset != 99 {
		t.Fatalf("MarkOffset offset: got %d, want 99", sess.marks[0].offset)
	}
	if sess.commits != 1 {
		t.Fatalf("Commit calls: got %d, want 1", sess.commits)
	}
}

// ---------------------------------------------------------------------------
// PeriodicCommitStrategy
// ---------------------------------------------------------------------------

func TestPeriodicCommitStrategy_ShouldCommit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		interval time.Duration
		sleep    time.Duration
		want     bool
	}{
		{
			name:     "interval_elapsed_commits",
			interval: time.Nanosecond,
			sleep:    2 * time.Millisecond,
			want:     true,
		},
		{
			name:     "interval_not_elapsed_no_commit",
			interval: time.Hour,
			sleep:    0,
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := NewPeriodicCommitStrategy(tt.interval, nullLogger())
			if tt.sleep > 0 {
				time.Sleep(tt.sleep)
			}
			got := s.ShouldCommit(0, 0, 0)
			if got != tt.want {
				t.Fatalf("ShouldCommit: got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPeriodicCommitStrategy_ZeroIntervalNormalized(t *testing.T) {
	t.Parallel()

	s := NewPeriodicCommitStrategy(0, nullLogger())
	if s.interval <= 0 {
		t.Fatal("zero interval must be normalized to a positive default")
	}
}

func TestPeriodicCommitStrategy_MarkAndCommit(t *testing.T) {
	t.Parallel()

	s := NewPeriodicCommitStrategy(time.Hour, nullLogger())
	sess := &mockSession{}

	s.MarkAndCommit(sess, "logs", 2, 77)

	if len(sess.marks) != 1 {
		t.Fatalf("MarkOffset calls: got %d, want 1", len(sess.marks))
	}
	if sess.marks[0].offset != 77 {
		t.Fatalf("MarkOffset offset: got %d, want 77", sess.marks[0].offset)
	}
	if sess.commits != 1 {
		t.Fatalf("Commit calls: got %d, want 1", sess.commits)
	}
}

func TestPeriodicCommitStrategy_Flush(t *testing.T) {
	t.Parallel()

	s := NewPeriodicCommitStrategy(time.Hour, nullLogger())
	sess := &mockSession{}

	s.Flush(sess, "logs", 2, 88)

	if len(sess.marks) != 1 {
		t.Fatalf("MarkOffset calls: got %d, want 1", len(sess.marks))
	}
	if sess.marks[0].offset != 88 {
		t.Fatalf("MarkOffset offset: got %d, want 88", sess.marks[0].offset)
	}
	if sess.commits != 1 {
		t.Fatalf("Commit calls: got %d, want 1", sess.commits)
	}
}

// ---------------------------------------------------------------------------
// HybridCommitStrategy
// ---------------------------------------------------------------------------

func TestHybridCommitStrategy_ShouldCommit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		step        int64
		interval    time.Duration
		sleep       time.Duration
		currentBase int64
		newBase     int64
		want        bool
	}{
		{
			name:        "step_trigger",
			step:        5,
			interval:    time.Hour,
			currentBase: 100,
			newBase:     105,
			want:        true,
		},
		{
			name:        "time_trigger",
			step:        1000,
			interval:    time.Nanosecond,
			sleep:       2 * time.Millisecond,
			currentBase: 100,
			newBase:     101,
			want:        true,
		},
		{
			name:        "neither_trigger",
			step:        100,
			interval:    time.Hour,
			currentBase: 100,
			newBase:     101,
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := NewHybridCommitStrategy(tt.step, tt.interval, nullLogger())
			if tt.sleep > 0 {
				time.Sleep(tt.sleep)
			}
			got := s.ShouldCommit(tt.currentBase, tt.newBase, 0)
			if got != tt.want {
				t.Fatalf("ShouldCommit: got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHybridCommitStrategy_ZeroDefaultsNormalized(t *testing.T) {
	t.Parallel()

	s := NewHybridCommitStrategy(0, 0, nullLogger())
	if s.step <= 0 {
		t.Fatal("zero step must be normalized to a positive default")
	}
	if s.interval <= 0 {
		t.Fatal("zero interval must be normalized to a positive default")
	}
}

func TestHybridCommitStrategy_MarkAndCommit(t *testing.T) {
	t.Parallel()

	s := NewHybridCommitStrategy(1, time.Hour, nullLogger())
	sess := &mockSession{}

	s.MarkAndCommit(sess, "clicks", 0, 55)

	if len(sess.marks) != 1 {
		t.Fatalf("MarkOffset calls: got %d, want 1", len(sess.marks))
	}
	if sess.marks[0].offset != 55 {
		t.Fatalf("MarkOffset offset: got %d, want 55", sess.marks[0].offset)
	}
	if sess.commits != 1 {
		t.Fatalf("Commit calls: got %d, want 1", sess.commits)
	}
}

func TestHybridCommitStrategy_Flush(t *testing.T) {
	t.Parallel()

	s := NewHybridCommitStrategy(1, time.Hour, nullLogger())
	sess := &mockSession{}

	s.Flush(sess, "clicks", 0, 66)

	if len(sess.marks) != 1 {
		t.Fatalf("MarkOffset calls: got %d, want 1", len(sess.marks))
	}
	if sess.marks[0].offset != 66 {
		t.Fatalf("MarkOffset offset: got %d, want 66", sess.marks[0].offset)
	}
	if sess.commits != 1 {
		t.Fatalf("Commit calls: got %d, want 1", sess.commits)
	}
}
