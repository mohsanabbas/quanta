package pipeline

import (
	"context"
	"testing"

	"quanta/internal/config"
	"quanta/sink"

	pb "quanta/api/proto/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// compileDLQ tests
// ---------------------------------------------------------------------------

// testDLQSink is a minimal sink registered under "test-dlq" for compiler tests.
type testDLQSink struct {
	configured bool
	published  *pb.Frame
	closed     bool
}

func (s *testDLQSink) Configure(_ context.Context, _ any) error {
	s.configured = true
	return nil
}
func (s *testDLQSink) Publish(_ context.Context, f *pb.Frame) error {
	s.published = f
	return nil
}
func (s *testDLQSink) Close(_ context.Context) error {
	s.closed = true
	return nil
}

var _ sink.Adapter = (*testDLQSink)(nil)

// capturedDLQSink holds the last instance created by the factory.
var capturedDLQSink *testDLQSink

func init() {
	sink.Register(sink.Registration{
		Name: "test-dlq",
		New: func() sink.Adapter {
			s := &testDLQSink{}
			capturedDLQSink = s
			return s
		},
		ConfigProto: func() any { return &struct{}{} },
	})
}

func TestCompileDLQ_Enabled(t *testing.T) {
	t.Parallel()

	r := NewRunner(NewAckCoordinator())
	cfg := config.PipelineConfig{
		DLQ: &config.DLQConfig{
			Enabled: true,
			Sink:    "test-dlq",
		},
	}

	err := compileDLQ(context.Background(), cfg, r)
	require.NoError(t, err)
	assert.True(t, r.coord.HasDLQ(), "coordinator must have DLQ after compileDLQ")
	assert.True(t, capturedDLQSink.configured, "DLQ sink must be configured")
	assert.NotNil(t, r.dlqSink, "runner must hold DLQ sink reference")
}

func TestCompileDLQ_Nil(t *testing.T) {
	t.Parallel()

	r := NewRunner(NewAckCoordinator())
	cfg := config.PipelineConfig{DLQ: nil}

	err := compileDLQ(context.Background(), cfg, r)
	require.NoError(t, err)
	assert.False(t, r.coord.HasDLQ(), "no DLQ when config is nil")
}

func TestCompileDLQ_Disabled(t *testing.T) {
	t.Parallel()

	r := NewRunner(NewAckCoordinator())
	cfg := config.PipelineConfig{
		DLQ: &config.DLQConfig{Enabled: false, Sink: "test-dlq"},
	}

	err := compileDLQ(context.Background(), cfg, r)
	require.NoError(t, err)
	assert.False(t, r.coord.HasDLQ(), "no DLQ when disabled")
}

func TestCompileDLQ_UnknownSink(t *testing.T) {
	t.Parallel()

	r := NewRunner(NewAckCoordinator())
	cfg := config.PipelineConfig{
		DLQ: &config.DLQConfig{Enabled: true, Sink: "nonexistent"},
	}

	err := compileDLQ(context.Background(), cfg, r)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nonexistent")
}
