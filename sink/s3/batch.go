package s3

import (
	"bytes"
	"sync"

	pb "quanta/api/proto/v1"
)

type batch struct {
	records     [][]byte
	checkpoints []*pb.CheckpointToken
	count       int
	size        int
	capacity    int
}

func newBatch(capacity int) *batch {
	return &batch{
		records:     make([][]byte, capacity),
		checkpoints: make([]*pb.CheckpointToken, capacity),
		capacity:    capacity,
	}
}

func (b *batch) append(value []byte, tok *pb.CheckpointToken) {
	b.records[b.count] = bytes.Clone(value)
	b.checkpoints[b.count] = tok
	b.size += len(value)
	b.count++
}

func (b *batch) full() bool { return b.count >= b.capacity }

func (b *batch) len() int { return b.count }

func (b *batch) reset() {
	clear(b.records[:b.count])
	clear(b.checkpoints[:b.count])
	b.count = 0
	b.size = 0
}

func newBatchPool(capacity int) *sync.Pool {
	return &sync.Pool{
		New: func() any {
			return newBatch(capacity)
		},
	}
}
