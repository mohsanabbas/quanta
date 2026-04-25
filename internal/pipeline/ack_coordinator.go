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

type Barrier interface {
	Complete()
	Abort()
}

type DLQPublisher interface {
	Publish(ctx context.Context, frame *pb.Frame) error
}

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

func (c *AckCoordinator) CommitNow(tok *pb.CheckpointToken) {
	if tok == nil {
		return
	}
	c.commit(tok)
}

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

func (c *AckCoordinator) Len() int {
	c.mu.Lock()
	n := len(c.barriers)
	c.mu.Unlock()
	return n
}

func (c *AckCoordinator) SetDLQSink(s DLQPublisher) {
	c.mu.Lock()
	c.dlqSink = s
	c.mu.Unlock()
}

func (c *AckCoordinator) HasDLQ() bool {
	c.mu.Lock()
	has := c.dlqSink != nil
	c.mu.Unlock()
	return has
}

func (c *AckCoordinator) Nack(ctx context.Context, frame *pb.Frame, cause error) {
	if frame == nil {
		return
	}
	tok := frame.Checkpoint
	key := tokenKey(tok)

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
