package batch

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	pb "quanta/api/proto/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func kafkaTok(topic string, partition int32, offset int64) *pb.CheckpointToken {
	return &pb.CheckpointToken{Kind: &pb.CheckpointToken_Kafka{
		Kafka: &pb.KafkaOffset{Topic: topic, Partition: partition, Offset: offset},
	}}
}

func TestFlusher_Add_Success(t *testing.T) {
	t.Parallel()

	var flushed []Record[int]
	flushFn := func(_ context.Context, records []Record[int]) error {
		flushed = records
		return nil
	}

	var acked []*pb.CheckpointToken
	cb := Callbacks{
		Ack: func(_ context.Context, tok *pb.CheckpointToken) {
			acked = append(acked, tok)
		},
	}

	f := NewFlusher(FlusherConfig{BatchSize: 3, FlushInterval: time.Hour}, flushFn, cb)
	ctx := context.Background()
	f.Start(ctx)

	tok1 := kafkaTok("t", 0, 1)
	tok2 := kafkaTok("t", 0, 2)

	require.NoError(t, f.Add(ctx, 1, tok1, &pb.Frame{}, 0))
	require.NoError(t, f.Add(ctx, 2, tok2, &pb.Frame{}, 0))
	require.NoError(t, f.Close(ctx))

	require.Len(t, flushed, 2)
	assert.Equal(t, 1, flushed[0].Data)
	assert.Equal(t, 2, flushed[1].Data)
	require.Len(t, acked, 2)
}

func TestFlusher_Add_BatchFull_Flushes(t *testing.T) {
	t.Parallel()

	var flushCount atomic.Int32
	flushFn := func(_ context.Context, _ []Record[int]) error {
		flushCount.Add(1)
		return nil
	}

	f := NewFlusher(FlusherConfig{BatchSize: 2, FlushInterval: time.Hour}, flushFn, Callbacks{})
	ctx := context.Background()
	f.Start(ctx)

	require.NoError(t, f.Add(ctx, 1, nil, nil, 0))
	require.NoError(t, f.Add(ctx, 2, nil, nil, 0))

	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, int32(1), flushCount.Load())

	require.NoError(t, f.Close(ctx))
}

func TestFlusher_Add_AfterClose(t *testing.T) {
	t.Parallel()

	f := NewFlusher(FlusherConfig{BatchSize: 10}, func(context.Context, []Record[int]) error { return nil }, Callbacks{})
	ctx := context.Background()
	f.Start(ctx)

	require.NoError(t, f.Close(ctx))

	err := f.Add(ctx, 1, nil, nil, 0)
	assert.ErrorIs(t, err, ErrFlusherClosed)
}

func TestFlusher_Add_ContextCancelled(t *testing.T) {
	t.Parallel()

	flushFn := func(ctx context.Context, _ []Record[int]) error {
		<-ctx.Done()
		return ctx.Err()
	}

	var nacked atomic.Int32
	cb := Callbacks{
		Nack: func(_ context.Context, _ *pb.Frame, _ error) {
			nacked.Add(1)
		},
	}

	f := NewFlusher(FlusherConfig{BatchSize: 1, FlushInterval: time.Hour}, flushFn, cb)
	ctx, cancel := context.WithCancel(context.Background())
	f.Start(ctx)

	require.NoError(t, f.Add(ctx, 1, nil, &pb.Frame{}, 0))
	time.Sleep(20 * time.Millisecond)

	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	err := f.Add(ctx, 2, nil, &pb.Frame{}, 0)
	if err != nil {
		assert.ErrorIs(t, err, context.Canceled)
	}

	closeCtx, closeCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer closeCancel()
	_ = f.Close(closeCtx)
}

func TestFlusher_Add_Concurrent(t *testing.T) {
	t.Parallel()

	var flushCount atomic.Int32
	var totalRecords atomic.Int32
	var mu sync.Mutex
	flushFn := func(_ context.Context, records []Record[int]) error {
		flushCount.Add(1)
		mu.Lock()
		totalRecords.Add(int32(len(records)))
		mu.Unlock()
		return nil
	}

	const goroutines = 20
	const addsPerGoroutine = 50
	batchSize := 10

	f := NewFlusher(FlusherConfig{BatchSize: batchSize, FlushInterval: time.Hour}, flushFn, Callbacks{})
	ctx := context.Background()
	f.Start(ctx)

	var wg sync.WaitGroup
	for i := range goroutines {
		wg.Go(func() {
			for j := range addsPerGoroutine {
				err := f.Add(ctx, i*1000+j, nil, nil, 0)
				assert.NoError(t, err)
			}
		})
	}
	wg.Wait()
	require.NoError(t, f.Close(ctx))

	expectedTotal := int32(goroutines * addsPerGoroutine)
	assert.Equal(t, expectedTotal, totalRecords.Load())
}

func TestFlusher_FlushInterval_Triggers(t *testing.T) {
	t.Parallel()

	var flushCount atomic.Int32
	flushFn := func(_ context.Context, _ []Record[int]) error {
		flushCount.Add(1)
		return nil
	}

	f := NewFlusher(FlusherConfig{BatchSize: 100, FlushInterval: 50 * time.Millisecond}, flushFn, Callbacks{})
	ctx := context.Background()
	f.Start(ctx)

	require.NoError(t, f.Add(ctx, 1, nil, nil, 0))
	time.Sleep(100 * time.Millisecond)

	assert.GreaterOrEqual(t, flushCount.Load(), int32(1))
	require.NoError(t, f.Close(ctx))
}

func TestFlusher_FlushFunc_Error_NacksAll(t *testing.T) {
	t.Parallel()

	flushErr := errors.New("flush failed")
	flushFn := func(_ context.Context, _ []Record[int]) error {
		return flushErr
	}

	var nacked []*pb.Frame
	var nackErrs []error
	cb := Callbacks{
		Nack: func(_ context.Context, f *pb.Frame, err error) {
			nacked = append(nacked, f)
			nackErrs = append(nackErrs, err)
		},
	}

	f := NewFlusher(FlusherConfig{BatchSize: 2, FlushInterval: time.Hour}, flushFn, cb)
	ctx := context.Background()
	f.Start(ctx)

	frame1 := &pb.Frame{Key: []byte("k1")}
	frame2 := &pb.Frame{Key: []byte("k2")}

	require.NoError(t, f.Add(ctx, 1, nil, frame1, 0))
	require.NoError(t, f.Add(ctx, 2, nil, frame2, 0))

	time.Sleep(50 * time.Millisecond)
	require.NoError(t, f.Close(ctx))

	require.Len(t, nacked, 2)
	assert.Equal(t, frame1, nacked[0])
	assert.Equal(t, frame2, nacked[1])
	assert.Equal(t, flushErr, nackErrs[0])
	assert.Equal(t, flushErr, nackErrs[1])
}

func TestFlusher_FlushFunc_Success_AcksAll(t *testing.T) {
	t.Parallel()

	flushFn := func(_ context.Context, _ []Record[int]) error {
		return nil
	}

	var acked []*pb.CheckpointToken
	cb := Callbacks{
		Ack: func(_ context.Context, tok *pb.CheckpointToken) {
			acked = append(acked, tok)
		},
	}

	f := NewFlusher(FlusherConfig{BatchSize: 2, FlushInterval: time.Hour}, flushFn, cb)
	ctx := context.Background()
	f.Start(ctx)

	tok1 := kafkaTok("t", 0, 1)
	tok2 := kafkaTok("t", 0, 2)

	require.NoError(t, f.Add(ctx, 1, tok1, nil, 0))
	require.NoError(t, f.Add(ctx, 2, tok2, nil, 0))

	time.Sleep(50 * time.Millisecond)
	require.NoError(t, f.Close(ctx))

	require.Len(t, acked, 2)
	assert.Equal(t, tok1, acked[0])
	assert.Equal(t, tok2, acked[1])
}

func TestFlusher_Close_FlushesPartial(t *testing.T) {
	t.Parallel()

	var flushed []Record[int]
	flushFn := func(_ context.Context, records []Record[int]) error {
		flushed = records
		return nil
	}

	f := NewFlusher(FlusherConfig{BatchSize: 10, FlushInterval: time.Hour}, flushFn, Callbacks{})
	ctx := context.Background()
	f.Start(ctx)

	require.NoError(t, f.Add(ctx, 1, nil, nil, 0))
	require.NoError(t, f.Add(ctx, 2, nil, nil, 0))
	require.NoError(t, f.Add(ctx, 3, nil, nil, 0))
	require.NoError(t, f.Close(ctx))

	require.Len(t, flushed, 3)
}

func TestFlusher_Close_Idempotent(t *testing.T) {
	t.Parallel()

	f := NewFlusher(FlusherConfig{BatchSize: 10}, func(context.Context, []Record[int]) error { return nil }, Callbacks{})
	ctx := context.Background()
	f.Start(ctx)

	require.NoError(t, f.Close(ctx))
	require.NoError(t, f.Close(ctx))
	require.NoError(t, f.Close(ctx))
}

func TestFlusher_Close_DrainsSealed(t *testing.T) {
	t.Parallel()

	var flushCount atomic.Int32
	flushFn := func(_ context.Context, _ []Record[int]) error {
		flushCount.Add(1)
		return nil
	}

	f := NewFlusher(FlusherConfig{BatchSize: 2, FlushInterval: time.Hour}, flushFn, Callbacks{})
	ctx := context.Background()
	f.Start(ctx)

	for i := range 10 {
		require.NoError(t, f.Add(ctx, i, nil, nil, 0))
	}

	require.NoError(t, f.Close(ctx))
	assert.Equal(t, int32(5), flushCount.Load())
}

func TestFlusher_Close_EmptyBatch(t *testing.T) {
	t.Parallel()

	var flushCount atomic.Int32
	flushFn := func(_ context.Context, _ []Record[int]) error {
		flushCount.Add(1)
		return nil
	}

	f := NewFlusher(FlusherConfig{BatchSize: 10}, flushFn, Callbacks{})
	ctx := context.Background()
	f.Start(ctx)

	require.NoError(t, f.Close(ctx))
	assert.Equal(t, int32(0), flushCount.Load())
}

func TestFlusher_Callbacks_Nil(t *testing.T) {
	t.Parallel()

	flushFn := func(_ context.Context, _ []Record[int]) error {
		return nil
	}

	f := NewFlusher(FlusherConfig{BatchSize: 2}, flushFn, Callbacks{})
	ctx := context.Background()
	f.Start(ctx)

	require.NoError(t, f.Add(ctx, 1, nil, nil, 0))
	require.NoError(t, f.Add(ctx, 2, nil, nil, 0))

	time.Sleep(50 * time.Millisecond)
	require.NoError(t, f.Close(ctx))
}

func TestFlusher_FlushFunc_Panic_NacksAll(t *testing.T) {
	t.Parallel()

	flushFn := func(_ context.Context, _ []Record[int]) error {
		panic("simulated panic")
	}

	var nackCount atomic.Int32
	cb := Callbacks{
		Nack: func(_ context.Context, _ *pb.Frame, _ error) {
			nackCount.Add(1)
		},
	}

	f := NewFlusher(FlusherConfig{BatchSize: 2, FlushInterval: time.Hour}, flushFn, cb)
	ctx := context.Background()
	f.Start(ctx)

	require.NoError(t, f.Add(ctx, 1, nil, &pb.Frame{}, 0))
	require.NoError(t, f.Add(ctx, 2, nil, &pb.Frame{}, 0))

	time.Sleep(50 * time.Millisecond)
	require.NoError(t, f.Close(ctx))

	assert.Equal(t, int32(2), nackCount.Load())
}

func TestFlusher_DefaultConfig(t *testing.T) {
	t.Parallel()

	f := NewFlusher(FlusherConfig{}, func(context.Context, []Record[int]) error { return nil }, Callbacks{})

	assert.Equal(t, _defaultBatchSize, f.cfg.BatchSize)
	assert.Equal(t, _defaultFlushInterval, f.cfg.FlushInterval)

	ctx := context.Background()
	f.Start(ctx)
	require.NoError(t, f.Close(ctx))
}

func TestFlusher_ContextCancelledDuringFlush(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())

	flushFn := func(fctx context.Context, _ []Record[int]) error {
		select {
		case <-fctx.Done():
			return fctx.Err()
		case <-time.After(100 * time.Millisecond):
			return nil
		}
	}

	f := NewFlusher(FlusherConfig{BatchSize: 1, FlushInterval: time.Hour}, flushFn, Callbacks{})
	f.Start(ctx)

	require.NoError(t, f.Add(ctx, 1, nil, nil, 0))

	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	closeCtx, closeCancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer closeCancel()
	err := f.Close(closeCtx)
	if err != nil {
		assert.ErrorIs(t, err, context.DeadlineExceeded)
	}
}
