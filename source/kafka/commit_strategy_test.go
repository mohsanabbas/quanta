package kafka

import (
	"log/slog"
	"testing"
	"time"

	"go.uber.org/mock/gomock"
)

func nullLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(noopWriter{}, nil))
}

type noopWriter struct{}

func (noopWriter) Write(p []byte) (int, error) { return len(p), nil }

func TestAckBasedCommitStrategy_ShouldCommit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		step        int64
		currentBase int64
		newBase     int64
		want        bool
	}{
		{name: "step_reached", step: 5, currentBase: 100, newBase: 105, want: true},
		{name: "step_not_reached", step: 5, currentBase: 100, newBase: 104, want: false},
		{name: "step_1_always_commits", step: 1, currentBase: 200, newBase: 201, want: true},
		{name: "zero_step_normalized_to_1", step: 0, currentBase: 10, newBase: 11, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := NewAckBasedCommitStrategy(tt.step, nullLogger())
			if got := s.ShouldCommit(tt.currentBase, tt.newBase, 0); got != tt.want {
				t.Fatalf("ShouldCommit: got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAckBasedCommitStrategy_ShouldCommit_ResetsAfterMarkAndCommit(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	sess := NewMockConsumerGroupSession(ctrl)
	sess.EXPECT().MarkOffset("t", int32(0), int64(105), "").Times(1)
	sess.EXPECT().Commit().Times(1)

	s := NewAckBasedCommitStrategy(5, nullLogger())

	if !s.ShouldCommit(100, 105, 0) {
		t.Fatal("expected ShouldCommit=true at step boundary")
	}
	s.MarkAndCommit(sess, "t", 0, 105)

	if s.ShouldCommit(105, 109, 0) {
		t.Fatal("expected ShouldCommit=false before next step boundary")
	}
	if !s.ShouldCommit(105, 110, 0) {
		t.Fatal("expected ShouldCommit=true at next step boundary")
	}
}

func TestAckBasedCommitStrategy_MarkAndCommit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		topic     string
		partition int32
		offset    int64
	}{
		{name: "orders_partition_3", topic: "orders", partition: 3, offset: 42},
		{name: "events_partition_0", topic: "events", partition: 0, offset: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			sess := NewMockConsumerGroupSession(ctrl)
			sess.EXPECT().MarkOffset(tt.topic, tt.partition, tt.offset, "").Times(1)
			sess.EXPECT().Commit().Times(1)

			s := NewAckBasedCommitStrategy(1, nullLogger())
			s.MarkAndCommit(sess, tt.topic, tt.partition, tt.offset)
		})
	}
}

func TestAckBasedCommitStrategy_Flush(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		topic     string
		partition int32
		offset    int64
	}{
		{name: "events_partition_1", topic: "events", partition: 1, offset: 99},
		{name: "logs_partition_0", topic: "logs", partition: 0, offset: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			sess := NewMockConsumerGroupSession(ctrl)
			sess.EXPECT().MarkOffset(tt.topic, tt.partition, tt.offset, "").Times(1)
			sess.EXPECT().Commit().Times(1)

			s := NewAckBasedCommitStrategy(1, nullLogger())
			s.Flush(sess, tt.topic, tt.partition, tt.offset)
		})
	}
}

func TestPeriodicCommitStrategy_ShouldCommit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		interval time.Duration
		sleep    time.Duration
		want     bool
	}{
		{name: "interval_elapsed_commits", interval: time.Nanosecond, sleep: 2 * time.Millisecond, want: true},
		{name: "interval_not_elapsed_no_commit", interval: time.Hour, sleep: 0, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := NewPeriodicCommitStrategy(tt.interval, nullLogger())
			if tt.sleep > 0 {
				time.Sleep(tt.sleep)
			}
			if got := s.ShouldCommit(0, 0, 0); got != tt.want {
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

	tests := []struct {
		name      string
		topic     string
		partition int32
		offset    int64
	}{
		{name: "logs_partition_2", topic: "logs", partition: 2, offset: 77},
		{name: "metrics_partition_1", topic: "metrics", partition: 1, offset: 500},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			sess := NewMockConsumerGroupSession(ctrl)
			sess.EXPECT().MarkOffset(tt.topic, tt.partition, tt.offset, "").Times(1)
			sess.EXPECT().Commit().Times(1)

			s := NewPeriodicCommitStrategy(time.Hour, nullLogger())
			s.MarkAndCommit(sess, tt.topic, tt.partition, tt.offset)
		})
	}
}

func TestPeriodicCommitStrategy_Flush(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		topic     string
		partition int32
		offset    int64
	}{
		{name: "logs_partition_2", topic: "logs", partition: 2, offset: 88},
		{name: "audit_partition_0", topic: "audit", partition: 0, offset: 1024},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			sess := NewMockConsumerGroupSession(ctrl)
			sess.EXPECT().MarkOffset(tt.topic, tt.partition, tt.offset, "").Times(1)
			sess.EXPECT().Commit().Times(1)

			s := NewPeriodicCommitStrategy(time.Hour, nullLogger())
			s.Flush(sess, tt.topic, tt.partition, tt.offset)
		})
	}
}

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
		{name: "step_trigger", step: 5, interval: time.Hour, currentBase: 100, newBase: 105, want: true},
		{name: "time_trigger", step: 1000, interval: time.Nanosecond, sleep: 2 * time.Millisecond, currentBase: 100, newBase: 101, want: true},
		{name: "neither_trigger", step: 100, interval: time.Hour, currentBase: 100, newBase: 101, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := NewHybridCommitStrategy(tt.step, tt.interval, nullLogger())
			if tt.sleep > 0 {
				time.Sleep(tt.sleep)
			}
			if got := s.ShouldCommit(tt.currentBase, tt.newBase, 0); got != tt.want {
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

	tests := []struct {
		name      string
		topic     string
		partition int32
		offset    int64
	}{
		{name: "clicks_partition_0", topic: "clicks", partition: 0, offset: 55},
		{name: "orders_partition_2", topic: "orders", partition: 2, offset: 200},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			sess := NewMockConsumerGroupSession(ctrl)
			sess.EXPECT().MarkOffset(tt.topic, tt.partition, tt.offset, "").Times(1)
			sess.EXPECT().Commit().Times(1)

			s := NewHybridCommitStrategy(1, time.Hour, nullLogger())
			s.MarkAndCommit(sess, tt.topic, tt.partition, tt.offset)
		})
	}
}

func TestHybridCommitStrategy_Flush(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		topic     string
		partition int32
		offset    int64
	}{
		{name: "clicks_partition_0", topic: "clicks", partition: 0, offset: 66},
		{name: "events_partition_3", topic: "events", partition: 3, offset: 999},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			sess := NewMockConsumerGroupSession(ctrl)
			sess.EXPECT().MarkOffset(tt.topic, tt.partition, tt.offset, "").Times(1)
			sess.EXPECT().Commit().Times(1)

			s := NewHybridCommitStrategy(1, time.Hour, nullLogger())
			s.Flush(sess, tt.topic, tt.partition, tt.offset)
		})
	}
}
