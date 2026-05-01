package batch

import (
	"sync"
	"testing"

	pb "quanta/api/proto/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestNew(t *testing.T) {
	t.Parallel()

	t.Run("positive capacity", func(t *testing.T) {
		t.Parallel()
		b := New[int](10)
		assert.Equal(t, 0, b.Len())
		assert.Equal(t, int64(0), b.ByteSize())
	})

	t.Run("zero capacity uses default", func(t *testing.T) {
		t.Parallel()
		b := New[int](0)
		for i := range 99 {
			assert.False(t, b.Append(i, nil, nil, 0))
		}
		assert.True(t, b.Append(99, nil, nil, 0))
	})

	t.Run("negative capacity uses default", func(t *testing.T) {
		t.Parallel()
		b := New[int](-5)
		assert.Equal(t, 0, b.Len())
	})
}

func TestBatch_Append(t *testing.T) {
	t.Parallel()

	t.Run("returns false until full", func(t *testing.T) {
		t.Parallel()
		b := New[string](3)

		assert.False(t, b.Append("a", nil, nil, 1))
		assert.False(t, b.Append("b", nil, nil, 2))
		assert.True(t, b.Append("c", nil, nil, 3))
		assert.Equal(t, 3, b.Len())
		assert.Equal(t, int64(6), b.ByteSize())
	})

	t.Run("stores checkpoint and frame", func(t *testing.T) {
		t.Parallel()
		b := New[[]byte](2)

		cp := &pb.CheckpointToken{
			Kind: &pb.CheckpointToken_Kafka{
				Kafka: &pb.KafkaOffset{Topic: "t", Partition: 0, Offset: 100},
			},
		}
		frame := &pb.Frame{
			Key:     []byte("key"),
			Value:   []byte("value"),
			Headers: nil,
			Ts: &timestamppb.Timestamp{
				Seconds: 0,
				Nanos:   0,
			},
			Checkpoint: &pb.CheckpointToken{
				Kind: nil,
			},
		}

		b.Append([]byte("data"), cp, frame, 4)
		records := b.Seal()

		require.Len(t, records, 1)
		assert.Equal(t, []byte("data"), records[0].Data)
		assert.Equal(t, cp, records[0].Checkpoint)
		assert.Equal(t, frame, records[0].Frame)
	})
}

func TestBatch_Append_Concurrent(t *testing.T) {
	t.Parallel()

	const goroutines = 50
	const appendsPerGoroutine = 20
	b := New[int](goroutines * appendsPerGoroutine * 2)

	var wg sync.WaitGroup
	for i := range goroutines {
		wg.Go(func() {
			for j := range appendsPerGoroutine {
				b.Append(i*1000+j, nil, nil, 1)
			}
		})
	}
	wg.Wait()

	assert.Equal(t, goroutines*appendsPerGoroutine, b.Len())
	assert.Equal(t, int64(goroutines*appendsPerGoroutine), b.ByteSize())
}

func TestBatch_Seal(t *testing.T) {
	t.Parallel()

	t.Run("returns records and resets", func(t *testing.T) {
		t.Parallel()
		b := New[int](10)

		b.Append(1, nil, nil, 10)
		b.Append(2, nil, nil, 20)

		records := b.Seal()
		require.Len(t, records, 2)
		assert.Equal(t, 1, records[0].Data)
		assert.Equal(t, 2, records[1].Data)
		assert.Equal(t, 0, b.Len())
		assert.Equal(t, int64(0), b.ByteSize())
	})

	t.Run("returns nil when empty", func(t *testing.T) {
		t.Parallel()
		b := New[int](10)
		assert.Nil(t, b.Seal())
	})

	t.Run("can reuse after seal", func(t *testing.T) {
		t.Parallel()
		b := New[int](2)

		b.Append(1, nil, nil, 0)
		b.Append(2, nil, nil, 0)
		_ = b.Seal()

		b.Append(3, nil, nil, 0)
		assert.Equal(t, 1, b.Len())
	})
}

func TestBatch_Reset(t *testing.T) {
	t.Parallel()

	b := New[int](10)
	b.Append(1, nil, nil, 10)
	b.Append(2, nil, nil, 20)

	b.Reset()

	assert.Equal(t, 0, b.Len())
	assert.Equal(t, int64(0), b.ByteSize())

	b.Append(3, nil, nil, 5)
	assert.Equal(t, 1, b.Len())
	assert.Equal(t, int64(5), b.ByteSize())
}

func TestBatch_Len(t *testing.T) {
	t.Parallel()

	b := New[int](10)
	assert.Equal(t, 0, b.Len())

	b.Append(1, nil, nil, 0)
	assert.Equal(t, 1, b.Len())

	b.Append(2, nil, nil, 0)
	assert.Equal(t, 2, b.Len())
}

func TestBatch_ByteSize(t *testing.T) {
	t.Parallel()

	b := New[int](10)
	assert.Equal(t, int64(0), b.ByteSize())

	b.Append(1, nil, nil, 100)
	assert.Equal(t, int64(100), b.ByteSize())

	b.Append(2, nil, nil, 50)
	assert.Equal(t, int64(150), b.ByteSize())
}
