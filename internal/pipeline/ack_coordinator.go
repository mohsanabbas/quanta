package pipeline

import (
	"bytes"
	"context"
	"log/slog"
	"math"
	"strconv"
	"sync"
	"sync/atomic"

	pb "quanta/api/proto/v1"
)

const (
	_barrierLive      int32 = 0
	_barrierCommitted int32 = 1
	_barrierAborted   int32 = 2
)

// Barrier controls the lifecycle of a single checkpoint commit decision.
type Barrier interface {
	Complete()
	Abort()
}

// DLQPublisher is the narrow interface the coordinator needs from a DLQ sink.
// Any sink.Adapter satisfies this implicitly — no coupling to the sink package.
type DLQPublisher interface {
	Publish(ctx context.Context, frame *pb.Frame) error
}

// AckCoordinator centralized checkpoint lifecycle manager.
// The coordinator is the ONLY component that commits checkpoints back to
// the source.
type AckCoordinator struct {
	mu       sync.Mutex
	subs     []func(*pb.ConnectorAck)
	barriers map[string]*ackBarrier
	dlFn     DeadLetterFn
	dlqSink  DLQPublisher
}

func NewAckCoordinator() *AckCoordinator {
	return &AckCoordinator{
		barriers: make(map[string]*ackBarrier),
	}
}

func (c *AckCoordinator) Subscribe(fn func(*pb.ConnectorAck)) {
	c.mu.Lock()
	c.subs = append(c.subs, fn)
	c.mu.Unlock()
}

func (c *AckCoordinator) SetDeadLetter(fn DeadLetterFn) {
	c.mu.Lock()
	c.dlFn = fn
	c.mu.Unlock()
}

// Barrier creates a refcounted completion barrier for a checkpoint.
// refs is the number of Complete/Ack calls required before commit.
//
// If a barrier already exists for this token (source redelivery), the
// stale barrier is force-aborted and replaced.
//
// Nil tokens produce a valid barrier that is not tracked  Complete and
// Abort are safe but no commit fires.
func (c *AckCoordinator) Barrier(tok *pb.CheckpointToken, refs int) Barrier {
	if refs < 0 || refs > math.MaxInt32 {
		slog.Warn("ackBarrier: refs out of int32 range, clamping", "refs", refs)
		refs = 0
	}
	b := &ackBarrier{token: tok, coord: c}
	b.refs.Store(int32(refs)) //nolint:gosec // bounds-checked above

	key := tokenKey(tok)
	if key == "" {
		return b
	}

	c.mu.Lock()
	if old, exists := c.barriers[key]; exists {
		slog.Warn("ackBarrier: duplicate key, aborting stale barrier", "key", key)
		old.state.CompareAndSwap(_barrierLive, _barrierAborted)
	}
	c.barriers[key] = b
	c.mu.Unlock()
	return b
}

// Ack is the callback given to AckAware sinks — satisfies sink.EmitFn.
// The context is forwarded from the sink for OTel trace/metric propagation.
// Thread-safe. No-op if no barrier exists or token is nil.
func (c *AckCoordinator) Ack(_ context.Context, tok *pb.CheckpointToken) {
	key := tokenKey(tok)
	if key == "" {
		return
	}
	c.mu.Lock()
	b := c.barriers[key]
	c.mu.Unlock()
	if b == nil {
		return
	}
	b.release()
}

// CommitNow immediately commits a checkpoint without creating a barrier.
// Used when all derived frames are dropped/failed and nothing reaches sinks.
// No-op for nil tokens.
func (c *AckCoordinator) CommitNow(tok *pb.CheckpointToken) {
	if tok == nil {
		return
	}
	c.commit(tok)
}

// Fail invokes the dead-letter handler for a permanently failed frame.
// The checkpoint lifecycle is handled by the caller pushFrame:
//   - All frames fail pushFrame calls CommitNow
//   - Some frames survive → the surviving barrier commits when sinks ack
//
// If the dead-letter callback itself returns an error, the failure is logged
// at error level. Previously the return value was discarded.
func (c *AckCoordinator) Fail(stage string, frame *pb.Frame, cause error) {
	slog.Error("permanent failure, dead-lettering frame",
		"stage", stage, "error", cause)
	c.mu.Lock()
	fn := c.dlFn
	c.mu.Unlock()
	if fn == nil {
		return
	}
	if err := fn(stage, frame, cause); err != nil {
		slog.Error("dead-letter callback failed",
			"stage", stage, "original_error", cause, "callback_error", err)
	}
}

// Len returns the number of outstanding unresolved barriers.
func (c *AckCoordinator) Len() int {
	c.mu.Lock()
	n := len(c.barriers)
	c.mu.Unlock()
	return n
}

// SetDLQSink configures the dead-letter queue publisher.
// Thread-safe; replaces any previously set DLQ sink.
func (c *AckCoordinator) SetDLQSink(s DLQPublisher) {
	c.mu.Lock()
	c.dlqSink = s
	c.mu.Unlock()
}

// HasDLQ returns whether a DLQ sink is configured.
func (c *AckCoordinator) HasDLQ() bool {
	c.mu.Lock()
	has := c.dlqSink != nil
	c.mu.Unlock()
	return has
}

// Nack handles permanent sink delivery failure for a frame.
//
// Behaviour:
//  1. Abort the barrier (no normal commit).
//  2. If a DLQ sink is configured, publish a DLQ frame.
//  3. On DLQ success commit checkpoint (source advances).
//  4. On DLQ failure or no DLQ withhold commit (source redelivers).
//
// Thread-safe. No-op for nil frames or nil checkpoints.
func (c *AckCoordinator) Nack(ctx context.Context, frame *pb.Frame, cause error) {
	if frame == nil {
		return
	}
	tok := frame.Checkpoint
	key := tokenKey(tok)

	// Abort the barrier so the normal ack path cannot commit.
	if key != "" {
		c.mu.Lock()
		if b := c.barriers[key]; b != nil {
			b.state.CompareAndSwap(_barrierLive, _barrierAborted)
			if c.barriers[key] == b {
				delete(c.barriers, key)
			}
		}
		c.mu.Unlock()
	}

	// No checkpoint → nothing to commit.
	if tok == nil {
		slog.Debug("nack: frame has nil checkpoint, nothing to commit or DLQ",
			"error", cause)
		return
	}

	c.mu.Lock()
	dlq := c.dlqSink
	c.mu.Unlock()

	if dlq == nil {
		slog.Warn("nack: no DLQ configured, withholding commit for redelivery",
			"key", key, "error", cause)
		return
	}

	dlqFrame := buildDLQFrame(frame, cause)
	if err := dlq.Publish(ctx, dlqFrame); err != nil {
		slog.Error("nack: DLQ publish failed, withholding commit for redelivery",
			"key", key, "dlq_error", err, "original_error", cause)
		return
	}

	c.commit(tok)
}

// buildDLQFrame wraps an original frame with error metadata for the DLQ.
func buildDLQFrame(original *pb.Frame, cause error) *pb.Frame {
	headers := make(map[string][]byte, len(original.Headers)+2)
	for k, v := range original.Headers {
		headers[k] = bytes.Clone(v)
	}
	headers["x-dlq-error"] = []byte(cause.Error())
	headers["x-dlq-original-key"] = bytes.Clone(original.Key)

	return &pb.Frame{
		Key:        original.Key,
		Value:      original.Value,
		Headers:    headers,
		Ts:         original.Ts,
		Checkpoint: original.Checkpoint,
	}
}

func (c *AckCoordinator) commit(tok *pb.CheckpointToken) {
	if tok == nil {
		return
	}
	ack := &pb.ConnectorAck{Checkpoint: tok}

	c.mu.Lock()
	handlers := make([]func(*pb.ConnectorAck), len(c.subs))
	copy(handlers, c.subs)
	c.mu.Unlock()

	for _, fn := range handlers {
		fn(ack)
	}
}

func (c *AckCoordinator) removeBarrier(b *ackBarrier) {
	key := tokenKey(b.token)
	if key == "" {
		return
	}
	c.mu.Lock()
	if c.barriers[key] == b {
		delete(c.barriers, key)
	}
	c.mu.Unlock()
}

var _ Barrier = (*ackBarrier)(nil)

type ackBarrier struct {
	token *pb.CheckpointToken
	refs  atomic.Int32
	state atomic.Int32
	coord *AckCoordinator
}

func (b *ackBarrier) Complete() {
	b.release()
}

// Abort transitions the barrier to aborted. No commit fires.
// Safe to call concurrently with release CAS arbitrates.
func (b *ackBarrier) Abort() {
	if b.state.CompareAndSwap(_barrierLive, _barrierAborted) {
		b.coord.removeBarrier(b)
	}
}

func (b *ackBarrier) release() {
	n := b.refs.Add(-1)
	if n < 0 {
		slog.Warn("ackBarrier: refcount underflow",
			"key", tokenKey(b.token), "refs", n)
	}
	if n <= 0 && b.state.CompareAndSwap(_barrierLive, _barrierCommitted) {
		b.coord.removeBarrier(b)
		b.coord.commit(b.token)
	}
}

func tokenKey(tok *pb.CheckpointToken) string {
	if tok == nil {
		return ""
	}
	switch k := tok.Kind.(type) {
	case *pb.CheckpointToken_Kafka:
		buf := make([]byte, 0, 64)
		buf = append(buf, "k:"...)
		buf = append(buf, k.Kafka.Topic...)
		buf = append(buf, '/')
		buf = strconv.AppendInt(buf, int64(k.Kafka.Partition), 10)
		buf = append(buf, '/')
		buf = strconv.AppendInt(buf, k.Kafka.Offset, 10)
		return string(buf)
	case *pb.CheckpointToken_Sqs:
		return "s:" + k.Sqs.Queue + "/" + k.Sqs.Handle
	case *pb.CheckpointToken_Http:
		return "h:" + k.Http.Id
	case *pb.CheckpointToken_Raw:
		return "r:" + string(k.Raw)
	default:
		return ""
	}
}
