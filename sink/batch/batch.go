package batch

import (
	"sync"

	pb "quanta/api/proto/v1"
)

// Record wraps sink specific data with checkpoint metadata.
type Record[T any] struct {
	Data       T
	Checkpoint *pb.CheckpointToken
	Frame      *pb.Frame
}

// Batch accumulates records until sealed.
type Batch[T any] struct {
	records  []Record[T]
	byteSize int64
	capacity int
	mu       sync.Mutex
}

// New creates a Batch with the given capacity.
func New[T any](capacity int) *Batch[T] {
	if capacity <= 0 {
		capacity = 100
	}
	return &Batch[T]{
		records:  make([]Record[T], 0, capacity),
		capacity: capacity,
	}
}

// Append adds a record. Returns true if batch is full.
func (b *Batch[T]) Append(data T, cp *pb.CheckpointToken, f *pb.Frame, byteSize int64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.records = append(b.records, Record[T]{Data: data, Checkpoint: cp, Frame: f})
	b.byteSize += byteSize
	return len(b.records) >= b.capacity
}

// Len returns current record count.
func (b *Batch[T]) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.records)
}

// ByteSize returns accumulated byte size.
func (b *Batch[T]) ByteSize() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.byteSize
}

// Seal extracts all records and resets the batch. Returns nil if empty.
func (b *Batch[T]) Seal() []Record[T] {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.records) == 0 {
		return nil
	}
	out := b.records
	b.records = make([]Record[T], 0, b.capacity)
	b.byteSize = 0
	return out
}

// Reset clears the batch without returning records.
func (b *Batch[T]) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.records = b.records[:0]
	b.byteSize = 0
}
