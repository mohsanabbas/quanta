package s3

import (
	"sync"
	"testing"

	pb "quanta/api/proto/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBatchAppendAndFull(t *testing.T) {
	tests := []struct {
		name     string
		cap      int
		give     int
		wantFull bool
		wantLen  int
	}{
		{name: "empty", cap: 3, give: 0, wantFull: false, wantLen: 0},
		{name: "partial", cap: 3, give: 2, wantFull: false, wantLen: 2},
		{name: "exactly full", cap: 3, give: 3, wantFull: true, wantLen: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := newBatch(tt.cap)

			for i := range tt.give {
				tok := &pb.CheckpointToken{}
				b.append([]byte{byte(i)}, tok, nil)
			}

			assert.Equal(t, tt.wantLen, b.len())
			assert.Equal(t, tt.wantFull, b.full())
		})
	}
}

func TestBatchClonesSafely(t *testing.T) {
	b := newBatch(4)

	src := []byte("original")
	b.append(src, &pb.CheckpointToken{}, nil)

	src[0] = 'X'
	assert.Equal(t, []byte("original"), b.records[0], "batch must hold an independent copy")
}

func TestBatchCheckpoints(t *testing.T) {
	b := newBatch(2)

	tok1 := &pb.CheckpointToken{}
	tok2 := &pb.CheckpointToken{}
	b.append([]byte("a"), tok1, nil)
	b.append([]byte("b"), tok2, nil)

	require.Len(t, b.checkpoints[:b.len()], 2)
	assert.Same(t, tok1, b.checkpoints[0])
	assert.Same(t, tok2, b.checkpoints[1])
}

func TestBatchReset(t *testing.T) {
	b := newBatch(2)
	b.append([]byte("a"), &pb.CheckpointToken{}, nil)
	b.append([]byte("b"), &pb.CheckpointToken{}, nil)

	b.reset()

	assert.Equal(t, 0, b.len())
	assert.False(t, b.full())

	assert.Equal(t, 2, cap(b.records))
	assert.Equal(t, 2, cap(b.checkpoints))
}

func TestBatchSizeTracksBytes(t *testing.T) {
	b := newBatch(10)
	b.append([]byte("hello"), &pb.CheckpointToken{}, nil)
	b.append([]byte("world!!!"), &pb.CheckpointToken{}, nil)

	assert.Equal(t, 13, b.size)
}

func TestBatchPool(t *testing.T) {
	pool := newBatchPool(4)

	b := pool.Get().(*batch)
	require.NotNil(t, b)
	assert.Equal(t, 4, cap(b.records))
	assert.Equal(t, 0, b.len())

	b.append([]byte("x"), &pb.CheckpointToken{}, nil)
	b.reset()
	pool.Put(b)

	b2 := pool.Get().(*batch)
	assert.Equal(t, 0, b2.len(), "pooled batch must be clean after reset")
}

func TestBatchPoolConcurrent(t *testing.T) {
	pool := newBatchPool(8)

	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			b := pool.Get().(*batch)
			b.append([]byte("data"), &pb.CheckpointToken{}, nil)
			b.reset()
			pool.Put(b)
		})
	}
	wg.Wait()
}
