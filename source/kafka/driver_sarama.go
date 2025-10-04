package kafka

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	pb "quanta/api/proto/v1"

	"github.com/IBM/sarama"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// SaramaDriver is an Adapter implementation that consumes records from Kafka
// using the Sarama library. It supports both auto and end‑to‑end commit
// semantics and applies backpressure based on message size and count. A
// SaramaDriver must be registered via Register() or RegisterDefaults() before
// it can be constructed through NewAdapter().
type SaramaDriver struct {
	cfg    Config
	mode   CommitMode
	tuning Tuning

	cl    sarama.Client
	group sarama.ConsumerGroup

	baseAttrs  []slog.Attr
	partitions sync.Map // string -> *partitionProcessor
}

// Configure sets up the SaramaDriver with the provided configuration. It
// creates the underlying Sarama client and consumer group. Sarama logging is
// wired through slog based on the SaramaVerbose flag.
func (d *SaramaDriver) Configure(ctx context.Context, cfg Config) error {
	d.cfg = cfg
	pub := cfg.Public()
	tun := cfg.Tuning()
	d.mode = pub.CommitMode
	d.tuning = tun

	if tun.WindowBits < 256 {
		return fmt.Errorf("kafka tuning: window_bits (%d) must be >= 256", tun.WindowBits)
	}
	if int64(tun.WindowBits) < tun.InFlightMsgs {
		return fmt.Errorf("kafka tuning: inflight_msgs (%d) must be <= window_bits (%d)", tun.InFlightMsgs, tun.WindowBits)
	}

	d.baseAttrs = []slog.Attr{
		slog.String("group_id", pub.GroupID),
		slog.String("commit_mode", string(pub.CommitMode)),
		slog.String("topics", strings.Join(pub.Topics, ",")),
		slog.String("brokers", strings.Join(pub.Brokers, ",")),
		slog.Bool("sarama_verbose", pub.SaramaVerbose),
		slog.String("backpressure_strategy", pub.BackpressureStrategy),
		slog.String("checkpoint_strategy", pub.CheckpointStrategy),
		slog.String("commit_strategy", pub.CommitStrategyType),
	}

	d.loggerWithContext(ctx).Info("configuring kafka source driver")

	// Parse Kafka version. An empty version means use the latest supported by Sarama.
	var ver sarama.KafkaVersion
	if pub.Version == "" {
		ver = sarama.MaxVersion
	} else {
		parsed, err := sarama.ParseKafkaVersion(pub.Version)
		if err != nil {
			d.loggerWithContext(ctx).Error("invalid kafka version", slog.String("error", err.Error()))
			return err
		}
		ver = parsed
	}

	sc := sarama.NewConfig()
	sc.Version = ver
	sc.Consumer.Return.Errors = true
	if pub.TLSEn {
		sc.Net.TLS.Enable = true
	}
	if pub.SASLUser != "" {
		sc.Net.SASL.Enable = true
		sc.Net.SASL.User = pub.SASLUser
		sc.Net.SASL.Password = pub.SASLPass
	}
	switch pub.StartFrom {
	case "oldest":
		sc.Consumer.Offsets.Initial = sarama.OffsetOldest
	default:
		sc.Consumer.Offsets.Initial = sarama.OffsetNewest
	}

	client, err := sarama.NewClient(pub.Brokers, sc)
	if err != nil {
		d.loggerWithContext(ctx).Error("failed to create sarama client", slog.String("error", err.Error()))
		return err
	}
	group, err := sarama.NewConsumerGroupFromClient(pub.GroupID, client)
	if err != nil {
		d.loggerWithContext(ctx).Error("failed to join consumer group", slog.String("error", err.Error()))
		client.Close()
		return err
	}

	d.cl = client
	d.group = group

	if pub.SaramaVerbose {
		sarama.Logger = &saramaSlogAdapter{logger: d.logger(slog.String("library", "sarama"))}
		d.loggerWithContext(ctx).Info("sarama verbose logging enabled")
	} else {
		sarama.Logger = &saramaNoopLogger{}
	}

	d.loggerWithContext(ctx).Info("kafka source driver configured")
	return nil
}

// Run starts the consumption loop for all configured topics. It blocks until
// the context is cancelled or an unrecoverable error occurs. Each call to
// Consume assigns partitions to partitionProcessor instances which handle
// individual message processing and ack tracking.
func (d *SaramaDriver) Run(ctx context.Context, emit EmitFunc) error {
	pub := d.cfg.Public()
	log := d.loggerWithContext(ctx, slog.String("stage", "run"), slog.String("group_id", pub.GroupID))
	handler := &groupHandler{driver: d, emit: emit}
	log.Info("starting kafka consume loop")
	for {
		if err := d.group.Consume(ctx, pub.Topics, handler); err != nil {
			if ctx.Err() != nil {
				log.Info("consume loop exiting due to context", slog.String("reason", ctx.Err().Error()))
				return ctx.Err()
			}
			log.Error("sarama consume returned error", slog.String("error", err.Error()))
			return err
		}
		if ctx.Err() != nil {
			log.Info("consume loop canceled", slog.String("reason", ctx.Err().Error()))
			return ctx.Err()
		}
	}
}

// Close shuts down the consumer group and underlying client. It does not
// release backpressure tokens or stop any partition goroutines; those are
// handled during partition revocation. Errors are logged but not returned.
func (d *SaramaDriver) Close(context.Context) error {
	log := d.logger(slog.String("stage", "close"))
	if d.group != nil {
		if err := d.group.Close(); err != nil {
			log.Error("failed to close consumer group", slog.String("error", err.Error()))
		}
	}
	if d.cl != nil {
		if err := d.cl.Close(); err != nil {
			log.Error("failed to close client", slog.String("error", err.Error()))
		}
	}
	log.Info("kafka source driver closed")
	return nil
}

// OnAck is invoked by the pipeline when a ConnectorAck is received. It
// forwards the ack to the appropriate partitionProcessor which will advance
// the commit window and release backpressure tokens accordingly.
func (d *SaramaDriver) OnAck(ack *pb.ConnectorAck) {
	if ack == nil || ack.Checkpoint == nil {
		return
	}
	kafkaAck := ack.Checkpoint.GetKafka()
	if kafkaAck == nil {
		return
	}
	key := partitionKey(kafkaAck.Topic, kafkaAck.Partition)
	value, ok := d.partitions.Load(key)
	if !ok {
		d.logger(slog.String("stage", "ack"), slog.String("status", "unknown_partition")).Warn(
			"received ack for partition not owned",
			slog.String("topic", kafkaAck.Topic),
			slog.Int("partition", int(kafkaAck.Partition)),
			slog.Int64("offset", kafkaAck.Offset),
		)
		return
	}
	pp := value.(*partitionProcessor)
	handle := AckHandle{offset: kafkaAck.Offset, bytes: 0} // bytes handled by checkpoint manager
	pp.OnAck(handle)
}

// groupHandler is a Sarama ConsumerGroupHandler that delegates message
// processing to partitionProcessor instances.
type groupHandler struct {
	driver *SaramaDriver
	emit   EmitFunc
}

// Setup is called at the beginning of a new session. We do nothing here.
func (*groupHandler) Setup(sarama.ConsumerGroupSession) error { return nil }

// Cleanup is called at the end of a session. We do nothing here because
// partitionProcessor.Shutdown handles flushing and releasing resources when
// partitions are revoked.
func (h *groupHandler) Cleanup(sarama.ConsumerGroupSession) error {
	return nil
}

// ConsumeClaim runs the partition loop for a single assigned partition. It
// constructs a partitionProcessor using the component factory, stores it in
// the driver's map, and processes messages until the claim is closed.
func (h *groupHandler) ConsumeClaim(sess sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	key := partitionKey(claim.Topic(), claim.Partition())

	// Create strategy components using package-level factory functions
	backpressureMgr, err := NewBackpressureManager(h.driver.cfg)
	if err != nil {
		return fmt.Errorf("create backpressure manager: %w", err)
	}

	checkpointMgr, err := NewCheckpointManager(h.driver.cfg)
	if err != nil {
		return fmt.Errorf("create checkpoint manager: %w", err)
	}

	logger := h.driver.logger(
		slog.String("topic", claim.Topic()),
		slog.Int("partition", int(claim.Partition())),
	)
	commitStrategy, err := NewCommitStrategy(h.driver.cfg, logger)
	if err != nil {
		return fmt.Errorf("create commit strategy: %w", err)
	}

	// Create the partition processor with injected dependencies
	pp := newPartitionProcessor(
		h.driver,
		sess,
		claim.Topic(),
		claim.Partition(),
		backpressureMgr,
		checkpointMgr,
		commitStrategy,
	)

	h.driver.partitions.Store(key, pp)
	defer func() {
		pp.Shutdown()
		h.driver.partitions.Delete(key)
	}()

	for {
		msg, ok, err := h.nextMessage(sess, claim)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			return err
		}
		if !ok {
			return nil
		}
		if err := pp.ProcessMessage(sess, msg, h.emit); err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			return err
		}
	}
}

// nextMessage reads the next message from the claim or returns false when the
// claim is closed or an error occurs.
func (h *groupHandler) nextMessage(sess sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) (*sarama.ConsumerMessage, bool, error) {
	select {
	case <-sess.Context().Done():
		return nil, false, sess.Context().Err()
	case msg, ok := <-claim.Messages():
		if !ok {
			return nil, false, nil
		}
		return msg, true, nil
	}
}

// partitionKey constructs a stable identifier for a topic/partition pair.
func partitionKey(topic string, partition int32) string {
	return fmt.Sprintf("%s/%d", topic, partition)
}

// logger constructs a slog logger with the driver's base attributes and the
// provided additional attributes. It ignores any context stored attributes.
func (d *SaramaDriver) logger(attrs ...slog.Attr) *slog.Logger {
	combined := append(append([]slog.Attr{}, d.baseAttrs...), attrs...)
	return logger(combined...)
}

// loggerWithContext constructs a slog logger that includes attributes from
// the context as well as the driver's base attributes and the provided
// additional attributes.
func (d *SaramaDriver) loggerWithContext(ctx context.Context, attrs ...slog.Attr) *slog.Logger {
	combined := append(append([]slog.Attr{}, d.baseAttrs...), attrs...)
	return loggerFromContext(ctx, combined...)
}

// estimateMessageSize computes an approximate size for a Sarama message by
// summing the lengths of the key, value and headers. A minimum size of one
// is returned to avoid zero‐weight acquisitions.
func estimateMessageSize(msg *sarama.ConsumerMessage) int64 {
	size := len(msg.Key) + len(msg.Value)
	for _, h := range msg.Headers {
		if h != nil {
			size += len(h.Key) + len(h.Value)
		}
	}
	if size <= 0 {
		size = 1
	}
	return int64(size)
}

// messageToFrame converts a Sarama message into a Frame for the pipeline.
// It copies the key and value to avoid retaining references to the broker
// buffers and constructs a CheckpointToken for end‑to‑end acknowledgments.
func messageToFrame(msg *sarama.ConsumerMessage) *pb.Frame {
	keyCopy := append([]byte(nil), msg.Key...)
	valueCopy := append([]byte(nil), msg.Value...)
	headers := toHeaderMapCopy(msg.Headers)
	return &pb.Frame{
		Key:        keyCopy,
		Value:      valueCopy,
		Headers:    headers,
		Ts:         timestamppb.New(msg.Timestamp),
		Checkpoint: toCheckpoint(msg),
	}
}

// toCheckpoint constructs a CheckpointToken for the provided message.
func toCheckpoint(msg *sarama.ConsumerMessage) *pb.CheckpointToken {
	return &pb.CheckpointToken{
		Kind: &pb.CheckpointToken_Kafka{
			Kafka: &pb.KafkaOffset{
				Topic:     msg.Topic,
				Partition: msg.Partition,
				Offset:    msg.Offset,
			},
		},
	}
}

// toHeaderMapCopy converts Sarama headers into a map of string keys to
// byte slices. It copies the header values to avoid retaining broker
// buffers. A nil map is returned if no headers are present.
func toHeaderMapCopy(src []*sarama.RecordHeader) map[string][]byte {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string][]byte, len(src))
	for _, h := range src {
		if h == nil {
			continue
		}
		key := string(h.Key)
		out[key] = append([]byte(nil), h.Value...)
	}
	return out
}

// saramaSlogAdapter wraps a slog logger to satisfy the Sarama logger
// interface. All Sarama log output is sent at Debug level.
type saramaSlogAdapter struct {
	logger *slog.Logger
}

func (s *saramaSlogAdapter) Print(v ...interface{}) {
	s.logger.Debug("sarama", slog.Any("args", v))
}

func (s *saramaSlogAdapter) Println(v ...interface{}) {
	s.logger.Debug("sarama", slog.Any("args", v))
}

func (s *saramaSlogAdapter) Printf(format string, v ...interface{}) {
	s.logger.Debug("sarama", slog.String("message", fmt.Sprintf(format, v...)))
}

// saramaNoopLogger discards all Sarama log output.
type saramaNoopLogger struct{}

func (saramaNoopLogger) Print(...interface{})          {}
func (saramaNoopLogger) Println(...interface{})        {}
func (saramaNoopLogger) Printf(string, ...interface{}) {}
