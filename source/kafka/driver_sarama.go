package kafka

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	pb "quanta/api/proto/v1"
	qerr "quanta/internal/errors"

	"github.com/IBM/sarama"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type SaramaDriver struct {
	cfg    Config
	mode   CommitMode
	tuning Tuning

	cl    sarama.Client
	group sarama.ConsumerGroup

	baseAttrs  []slog.Attr
	partitions sync.Map
}

var _ Adapter = (*SaramaDriver)(nil)

func (d *SaramaDriver) Configure(ctx context.Context, cfg Config) error {
	d.cfg = cfg
	pub := cfg.Public()
	tun := cfg.Tuning()
	d.mode = pub.CommitMode
	d.tuning = tun

	if tun.WindowBits < _minWindowBits {
		return qerr.Config("kafka", "validate",
			fmt.Errorf("window_bits (%d) must be >= %d", tun.WindowBits, _minWindowBits))
	}
	if int64(tun.WindowBits) < tun.InFlightMsgs {
		return qerr.Config("kafka", "validate",
			fmt.Errorf("inflight_msgs (%d) must be <= window_bits (%d)", tun.InFlightMsgs, tun.WindowBits))
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
		sarama.Logger = &saramaSlogAdapter{logger: d.loggerWithContext(ctx, slog.String("library", "sarama"))}
		d.loggerWithContext(ctx).Info("sarama verbose logging enabled")
	} else {
		sarama.Logger = &saramaNoopLogger{}
	}

	d.loggerWithContext(ctx).Info("kafka source driver configured")
	return nil
}

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

func (d *SaramaDriver) Close(ctx context.Context) error {
	log := d.loggerWithContext(ctx, slog.String("stage", "close"))
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
	handle := AckHandle{offset: kafkaAck.Offset, bytes: 0}
	pp.OnAck(handle)
}

type groupHandler struct {
	driver *SaramaDriver
	emit   EmitFunc
}

func (*groupHandler) Setup(sarama.ConsumerGroupSession) error { return nil }

func (h *groupHandler) Cleanup(sarama.ConsumerGroupSession) error {
	return nil
}

func (h *groupHandler) ConsumeClaim(sess sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	key := partitionKey(claim.Topic(), claim.Partition())

	backpressureMgr, err := NewBackpressureManager(h.driver.cfg)
	if err != nil {
		return qerr.Source("kafka", "backpressure", err)
	}

	checkpointMgr, err := NewCheckpointManager(h.driver.cfg)
	if err != nil {
		return qerr.Source("kafka", "checkpoint", err)
	}

	logger := h.driver.logger(
		slog.String("topic", claim.Topic()),
		slog.Int("partition", int(claim.Partition())),
	)
	commitStrategy, err := NewCommitStrategy(h.driver.cfg, logger)
	if err != nil {
		return qerr.Source("kafka", "commit", err)
	}

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

func partitionKey(topic string, partition int32) string {
	return fmt.Sprintf("%s/%d", topic, partition)
}

func (d *SaramaDriver) logger(attrs ...slog.Attr) *slog.Logger {
	combined := append(append([]slog.Attr{}, d.baseAttrs...), attrs...)
	return logger(combined...)
}

func (d *SaramaDriver) loggerWithContext(ctx context.Context, attrs ...slog.Attr) *slog.Logger {
	combined := append(append([]slog.Attr{}, d.baseAttrs...), attrs...)
	return loggerFromContext(ctx, combined...)
}

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

type saramaNoopLogger struct{}

func (saramaNoopLogger) Print(...interface{})          {}
func (saramaNoopLogger) Println(...interface{})        {}
func (saramaNoopLogger) Printf(string, ...interface{}) {}
