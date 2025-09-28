package kafka

import (
	"math/bits"
	"sync"
	"sync/atomic"
)

// InvalidSlot is returned by PartitionTracker.Reserve when the requested slot
// would exceed the configured window size. It signals that the caller
// attempted to have more in‑flight messages than the window can track.
const InvalidSlot = ^uint32(0)

// PartitionTracker maintains a sliding bitset window of acked offsets for a
// single Kafka partition. The tracker is safe for concurrent use by a single
// reader and multiple ackers: Reserve reads the base atomically while
// AckOffset mutates state under a mutex. A zero value is not ready for
// operation call Reset() with the initial offset when a partition is
// assigned.
type PartitionTracker struct {
	mu          sync.Mutex
	base        int64
	window      []uint64
	size        uint32
	initialized bool
}

// NewPartitionTracker constructs a new tracker capable of holding
// windowBits bits of in‑flight state. If windowBits is zero, a default size
// of 4096 bits is used.
func NewPartitionTracker(windowBits uint32) *PartitionTracker {
	if windowBits == 0 {
		windowBits = 4096
	}
	words := int((windowBits + 63) / 64)
	return &PartitionTracker{
		base:   -1,
		window: make([]uint64, words),
		size:   windowBits,
	}
}

// Reset clears the window and sets the base offset. It marks the tracker as
// initialized and must be called once on assignment of a new partition.
func (p *PartitionTracker) Reset(base int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := range p.window {
		p.window[i] = 0
	}
	atomic.StoreInt64(&p.base, base)
	p.initialized = true
}

// Reserve returns the slot index within the window for the given offset or
// InvalidSlot if the offset would fall outside of the window. The first call
// to Reserve for a partition will implicitly initialize the base to the
// provided offset. Subsequent calls read the base atomically and perform a
// simple delta check without locking for common cases.
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
	if delta < 0 {
		return InvalidSlot
	}
	slot := uint32(delta)
	if slot >= p.size {
		return InvalidSlot
	}
	return slot
}

// AckOffset marks the given offset as acknowledged and returns the new base
// offset along with a boolean indicating whether the base advanced. If the
// offset lies outside of the current window the call is ignored and the
// current base is returned. AckOffset is safe to call concurrently with
// Reserve.
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
	if delta >= int64(p.size) {
		return base, false
	}
	slot := uint32(delta)
	word := slot / 64
	bit := slot % 64

	// Only mark as acked if not already acked | idempotent
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

// Base returns the current base offset. The base is updated atomically when
// the contiguous run of acked bits at the start of the window advances.
func (p *PartitionTracker) Base() int64 {
	return atomic.LoadInt64(&p.base)
}

// Initialized reports whether Reset() has been called or the tracker has
// implicitly initialized itself via Reserve() on first use.
func (p *PartitionTracker) Initialized() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.initialized
}

// advanceLocked inspects the bitset and advances the base offset while a run
// of ones exists at the least significant bits of the window. It must be
// called with p.mu held and updates neither p.base nor p.initialized.
func (p *PartitionTracker) advanceLocked(current int64) (int64, bool) {
	base := current
	advanced := false
	for {
		if len(p.window) == 0 || p.window[0] == 0 {
			return base, advanced
		}
		// Determine how many contiguous ones appear at the start of the bitset.
		run := bits.TrailingZeros64(^p.window[0])
		if run == 0 {
			return base, advanced
		}
		advanced = true
		shift := uint(run)
		var carry uint64
		// Shift the entire bitset right by 'run' bits.
		for i := len(p.window) - 1; i >= 0; i-- {
			next := p.window[i] << (64 - shift)
			p.window[i] = (p.window[i] >> shift) | carry
			carry = next
			if i == 0 {
				break
			}
		}
		base += int64(shift)
	}
}
