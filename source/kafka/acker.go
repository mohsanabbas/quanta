package kafka

import "sync"

// ackHandle holds metadata about an in‑flight record. The offset identifies
// the Kafka message and bytes records the size of the message for
// backpressure accounting. When an ack is received both pieces of
// information are required to advance the commit window and release tokens.
type ackHandle struct {
	offset int64
	bytes  int64
}

// Acker tracks in‑flight records by offset. It maps offsets to ack handles
// so that when a ConnectorAck arrives the driver can determine both the
// committed offset and how many backpressure tokens to release. Acker is
// safe for concurrent use.
type Acker struct {
	mu   sync.Mutex
	m    map[int64]ackHandle
	open bool
}

// NewAcker allocates an Acker with an initial capacity hint. The hint is
// advisory; passing zero or a negative number defaults to a reasonable size.
func NewAcker(capHint int) *Acker {
	if capHint <= 0 {
		capHint = 1024
	}
	return &Acker{
		m:    make(map[int64]ackHandle, capHint),
		open: true,
	}
}

// Track registers an in‑flight record with its ack handle. If the Acker has
// been closed calls to Track are ignored.
func (a *Acker) Track(offset int64, h ackHandle) {
	a.mu.Lock()
	if a.open {
		a.m[offset] = h
	}
	a.mu.Unlock()
}

// Ack removes and returns the handle for the given offset. The boolean
// return reports whether the offset was found. Calls after Close() will
// always return false.
func (a *Acker) Ack(offset int64) (ackHandle, bool) {
	a.mu.Lock()
	h, ok := a.m[offset]
	if ok {
		delete(a.m, offset)
	}
	a.mu.Unlock()
	return h, ok
}

// Remove behaves like Ack but is intended for error paths where an
// acknowledged record was never emitted downstream. It removes the handle and
// returns it if present.
func (a *Acker) Remove(offset int64) (ackHandle, bool) {
	a.mu.Lock()
	h, ok := a.m[offset]
	if ok {
		delete(a.m, offset)
	}
	a.mu.Unlock()
	return h, ok
}

// Reset drains the pending map and returns all handles in arbitrary order.
// It is used when a partition is revoked to release any outstanding
// backpressure tokens. The Acker remains open after a reset.
func (a *Acker) Reset() []ackHandle {
	a.mu.Lock()
	defer a.mu.Unlock()
	handles := make([]ackHandle, 0, len(a.m))
	for _, h := range a.m {
		handles = append(handles, h)
	}
	a.m = make(map[int64]ackHandle, len(handles))
	return handles
}

// Close marks the Acker as closed and clears all pending entries. Subsequent
// calls to Track will be ignored and calls to Ack will return false.
func (a *Acker) Close() {
	a.mu.Lock()
	a.open = false
	a.m = nil
	a.mu.Unlock()
}
