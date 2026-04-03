package kafka

import (
	"math"
	"math/bits"
	"sync"
	"sync/atomic"
)

const (
	InvalidSlot = ^uint32(0)

	_bitsPerWord = 64
)

type PartitionTracker struct {
	mu          sync.Mutex
	base        int64
	window      []uint64
	size        uint32
	initialized bool
}

func NewPartitionTracker(windowBits uint32) *PartitionTracker {
	if windowBits == 0 {
		windowBits = 4096
	}
	words := int((windowBits + _bitsPerWord - 1) / _bitsPerWord)
	return &PartitionTracker{
		base:   -1,
		window: make([]uint64, words),
		size:   windowBits,
	}
}

func (p *PartitionTracker) Reset(base int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := range p.window {
		p.window[i] = 0
	}
	atomic.StoreInt64(&p.base, base)
	p.initialized = true
}

func (p *PartitionTracker) Reserve(offset int64) uint32 {
	if !p.Initialized() {
		p.mu.Lock()
		if !p.initialized {
			atomic.StoreInt64(&p.base, offset)
			p.initialized = true
			for i := range p.window {
				p.window[i] = 0
			}
			p.mu.Unlock()
			return 0
		}
		p.mu.Unlock()
	}
	base := atomic.LoadInt64(&p.base)
	delta := offset - base
	if delta < 0 || delta > math.MaxUint32 {
		return InvalidSlot
	}
	slot := uint32(delta)
	if slot >= p.size {
		return InvalidSlot
	}
	return slot
}

func (p *PartitionTracker) AckOffset(offset int64) (int64, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.initialized {
		return p.base, false
	}
	base := atomic.LoadInt64(&p.base)
	if offset < base {
		return base, false
	}
	delta := offset - base
	if delta >= int64(p.size) || delta < 0 {
		return base, false
	}
	slot := uint32(delta) //nolint:gosec // bounds-checked above: 0 <= delta < p.size (uint32)
	word := slot / _bitsPerWord
	bit := slot % _bitsPerWord

	oldWord := p.window[word]
	mask := uint64(1) << bit
	if oldWord&mask != 0 {
		return base, false
	}

	p.window[word] |= mask
	newBase, advanced := p.advanceLocked(base)
	if advanced {
		atomic.StoreInt64(&p.base, newBase)
	}
	return newBase, advanced
}

func (p *PartitionTracker) Base() int64 {
	return atomic.LoadInt64(&p.base)
}

func (p *PartitionTracker) Initialized() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.initialized
}

func (p *PartitionTracker) advanceLocked(current int64) (int64, bool) {
	base := current
	advanced := false
	for {
		if len(p.window) == 0 || p.window[0] == 0 {
			return base, advanced
		}
		run := bits.TrailingZeros64(^p.window[0])
		if run == 0 {
			return base, advanced
		}
		advanced = true
		shift := uint(run) //nolint:gosec // run is 0..64 from TrailingZeros64
		var carry uint64
		for i := len(p.window) - 1; i >= 0; i-- {
			next := p.window[i] << (_bitsPerWord - shift)
			p.window[i] = (p.window[i] >> shift) | carry
			carry = next
			if i == 0 {
				break
			}
		}
		base += int64(shift) //nolint:gosec // shift is 0..64, fits int64
	}
}
