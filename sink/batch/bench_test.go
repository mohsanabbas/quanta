package batch

import (
	"context"
	"testing"

	pb "quanta/api/proto/v1"
)

// BenchmarkBatch_Append measures allocation per append.
func BenchmarkBatch_Append(b *testing.B) {
	batch := New[[]byte](1000)
	data := make([]byte, 256)
	cp := &pb.CheckpointToken{Kind: &pb.CheckpointToken_Kafka{
		Kafka: &pb.KafkaOffset{Topic: "bench", Partition: 0, Offset: 0},
	}}
	frame := &pb.Frame{Key: []byte("k"), Value: data}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		batch.Append(data, cp, frame, 256)
		if batch.Len() >= 1000 {
			batch.Reset()
		}
	}
}

// BenchmarkBatch_Seal measures allocation per seal.
func BenchmarkBatch_Seal(b *testing.B) {
	data := make([]byte, 256)
	cp := &pb.CheckpointToken{Kind: &pb.CheckpointToken_Kafka{
		Kafka: &pb.KafkaOffset{Topic: "bench", Partition: 0, Offset: 0},
	}}
	frame := &pb.Frame{Key: []byte("k"), Value: data}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		batch := New[[]byte](100)
		for j := 0; j < 100; j++ {
			batch.Append(data, cp, frame, 256)
		}
		_ = batch.Seal()
	}
}

// BenchmarkFlusher_Add measures allocation per add (no flush).
func BenchmarkFlusher_Add(b *testing.B) {
	flushFn := func(_ context.Context, _ []Record[[]byte]) error {
		return nil
	}

	f := NewFlusher(FlusherConfig{BatchSize: 10000, FlushInterval: 1<<31 - 1}, flushFn, Callbacks{})
	ctx := context.Background()
	f.Start(ctx)
	defer f.Close(ctx)

	data := make([]byte, 256)
	cp := &pb.CheckpointToken{Kind: &pb.CheckpointToken_Kafka{
		Kafka: &pb.KafkaOffset{Topic: "bench", Partition: 0, Offset: 0},
	}}
	frame := &pb.Frame{Key: []byte("k"), Value: data}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = f.Add(ctx, data, cp, frame, 256)
	}
}

// BenchmarkFlusher_AddFlush measures allocation per add with flush.
func BenchmarkFlusher_AddFlush(b *testing.B) {
	flushFn := func(_ context.Context, _ []Record[[]byte]) error {
		return nil
	}

	f := NewFlusher(FlusherConfig{BatchSize: 100, FlushInterval: 1<<31 - 1}, flushFn, Callbacks{})
	ctx := context.Background()
	f.Start(ctx)
	defer f.Close(ctx)

	data := make([]byte, 256)
	cp := &pb.CheckpointToken{Kind: &pb.CheckpointToken_Kafka{
		Kafka: &pb.KafkaOffset{Topic: "bench", Partition: 0, Offset: 0},
	}}
	frame := &pb.Frame{Key: []byte("k"), Value: data}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = f.Add(ctx, data, cp, frame, 256)
	}
}

// BenchmarkPool_GetPut measures pool allocation efficiency.
func BenchmarkPool_GetPut(b *testing.B) {
	pool := NewPool[[]byte](100)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		batch := pool.Get()
		pool.Put(batch)
	}
}
