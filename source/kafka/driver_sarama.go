package kafka

import (
	"context"
	"log/slog"
	"strings"

	pb "quanta/api/proto/v1"

	"github.com/IBM/sarama"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type recordID struct {
	topic     string
	partition int32
	offset    int64
}

type SaramaDriver struct {
	cfg   Config
	mode  CommitMode
	cl    sarama.Client
	group sarama.ConsumerGroup

	limiter     *Controller
	checkpoints *Manager[struct{}]
	ackManager  *ackTracker[recordID]

	baseAttrs []slog.Attr
}

func (d *SaramaDriver) Configure(ctx context.Context, cfg Config) error {
	d.cfg = cfg
	d.mode = cfg.CommitMode
	d.baseAttrs = []slog.Attr{
		slog.String("group_id", cfg.GroupID),
		slog.String("commit_mode", string(cfg.CommitMode)),
		slog.String("topics", strings.Join(cfg.Topics, ",")),
		slog.String("brokers", strings.Join(cfg.Brokers, ",")),
		slog.Bool("sarama_verbose", cfg.SaramaVerbose),
	}
	d.loggerWithContext(ctx).Info("configuring kafka source driver")

	d.limiter = NewController(cfg.BackPressure.Capacity)
	d.checkpoints = NewManager[struct{}](cfg.BackPressure.Capacity, cfg.Checkpoint.CommitInt)
	d.ackManager = newAckTracker[recordID]()

	ver, err := sarama.ParseKafkaVersion(cfg.Version)
	if err != nil {
		d.loggerWithContext(ctx).Error("invalid kafka version", slog.String("error", err.Error()))
		return err
	}

	sc := sarama.NewConfig()
	sc.Version = ver
	sc.Consumer.Return.Errors = true
	if cfg.TLSEn {
		sc.Net.TLS.Enable = true
	}
	if cfg.SASLUser != "" {
		sc.Net.SASL.Enable = true
		sc.Net.SASL.User = cfg.SASLUser
		sc.Net.SASL.Password = cfg.SASLPass
	}
	switch cfg.StartFrom {
	case "oldest":
		sc.Consumer.Offsets.Initial = sarama.OffsetOldest
	default:
		sc.Consumer.Offsets.Initial = sarama.OffsetNewest
	}

	client, err := sarama.NewClient(cfg.Brokers, sc)
	if err != nil {
		d.loggerWithContext(ctx).Error("failed to create sarama client", slog.String("error", err.Error()))
		return err
	}
	group, err := sarama.NewConsumerGroupFromClient(cfg.GroupID, client)
	if err != nil {
		d.loggerWithContext(ctx).Error("failed to join consumer group", slog.String("error", err.Error()))
		client.Close()
		return err
	}

	d.cl = client
	d.group = group
	if cfg.SaramaVerbose {
		sarama.Logger = &saramaSlogAdapter{
			logger: d.logger(slog.String("library", "sarama")),
		}
		d.loggerWithContext(ctx).Info("sarama verbose logging enabled")
	} else {
		sarama.Logger = &saramaNoopLogger{}
	}
	d.loggerWithContext(ctx).Info("kafka source driver configured")
	return nil
}

func (d *SaramaDriver) Run(ctx context.Context, emit EmitFunc) error {
	log := loggerFromContext(ctx, slog.String("stage", "run"), slog.String("group_id", d.cfg.GroupID))
	if err := d.ackManager.Start(ctx); err != nil {
		log.Error("failed to start ack tracker", slog.String("error", err.Error()))
		return err
	}

	handler := &groupHandler{
		driver: d,
		emit:   emit,
	}
	log.Info("starting kafka consume loop")
	for {
		if err := d.group.Consume(ctx, d.cfg.Topics, handler); err != nil {
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
	if d.limiter != nil {
		d.limiter.Close()
	}
	if d.ackManager != nil {
		d.ackManager.Close()
	}
	log.Info("kafka source driver closed")
	return nil
}

type groupHandler struct {
	driver *SaramaDriver
	emit   EmitFunc
}

func (*groupHandler) Setup(sarama.ConsumerGroupSession) error {
	return nil
}

func (h *groupHandler) Cleanup(_ sarama.ConsumerGroupSession) error {
	dropped := h.driver.ackManager.Reset()
	if dropped > 0 {
		h.driver.limiter.Release(int64(dropped))
		h.driver.logger(slog.String("stage", "cleanup")).Info(
			"released tokens for pending acks",
			slog.Int("dropped_ack_callbacks", dropped),
		)
	}
	h.driver.checkpoints.Reset(h.driver.cfg.BackPressure.Capacity)
	return nil
}

func (h *groupHandler) ConsumeClaim(sess sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for {
		if err := h.driver.limiter.Acquire(sess.Context()); err != nil {
			loggerFromContext(sess.Context(), slog.String("stage", "backpressure")).Error(
				"failed to acquire limiter token",
				slog.String("error", err.Error()),
			)
			return err
		}

		msg, ok, err := h.nextMessage(sess, claim)
		if err != nil {
			h.driver.limiter.Release(1)
			loggerFromContext(sess.Context(), slog.String("stage", "consume"), slog.String("result", "error")).Error(
				"failed to read message",
				slog.String("error", err.Error()),
			)
			return err
		}
		if !ok {
			h.driver.limiter.Release(1)
			loggerFromContext(sess.Context(), slog.String("stage", "consume"), slog.String("result", "closed")).Debug(
				"claim closed by broker",
			)
			return nil
		}

		releaseNow, err := h.processMessage(sess, msg)
		if releaseNow {
			h.driver.limiter.Release(1)
		}
		if err != nil {
			loggerFromContext(sess.Context(), slog.String("stage", "process")).Error(
				"failed to process message",
				slog.String("error", err.Error()),
			)
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

func (h *groupHandler) processMessage(sess sarama.ConsumerGroupSession, msg *sarama.ConsumerMessage) (bool, error) {
	resolve, err := h.driver.checkpoints.Track(sess.Context(), struct{}{})
	if err != nil {
		loggerFromContext(sess.Context(), slog.String("stage", "checkpoint")).Error(
			"failed to track checkpoint",
			slog.String("error", err.Error()),
		)
		return true, err
	}

	frame := &pb.Frame{
		Key:        append([]byte(nil), msg.Key...),
		Value:      append([]byte(nil), msg.Value...),
		Headers:    toHeaderMapCopy(msg.Headers),
		Ts:         timestamppb.New(msg.Timestamp),
		Checkpoint: toCheckpoint(msg),
	}

	if h.driver.mode == CommitAuto {
		if err := h.emit(sess.Context(), frame); err != nil {
			loggerFromContext(sess.Context(), slog.String("stage", "emit"), slog.String("mode", "auto")).Error(
				"failed to emit frame",
				slog.String("error", err.Error()),
			)
			return true, err
		}
		sess.MarkMessage(msg, "")
		if _, due := resolve(); due {
			sess.Commit()
		}
		return true, nil
	}

	rec := recordID{
		topic:     msg.Topic,
		partition: msg.Partition,
		offset:    msg.Offset,
	}
	msgCopy := *msg
	sessCtx := sess.Context()
	h.driver.ackManager.Track(rec, func() {
		defer h.driver.limiter.Release(1)

		if err := sessCtx.Err(); err != nil {
			loggerFromContext(sessCtx, slog.String("stage", "ack"), slog.String("status", "late"), slog.String("reason", err.Error())).Warn(
				"ack received after session ended",
				slog.String("topic", rec.topic),
				slog.Int("partition", int(rec.partition)),
				slog.Int64("offset", rec.offset),
			)
			return
		}

		sess.MarkMessage(&msgCopy, "")
		if _, due := resolve(); due {
			sess.Commit()
		}
		loggerFromContext(sessCtx, slog.String("stage", "ack"), slog.String("status", "applied"), slog.String("mode", "e2e")).Debug(
			"ack applied and token released",
			slog.String("topic", rec.topic),
			slog.Int("partition", int(rec.partition)),
			slog.Int64("offset", rec.offset),
		)
	})

	if err := h.emit(sess.Context(), frame); err != nil {
		h.driver.ackManager.Cancel(rec)
		loggerFromContext(sess.Context(), slog.String("stage", "emit"), slog.String("mode", "e2e")).Error(
			"failed to emit frame",
			slog.String("error", err.Error()),
			slog.String("topic", rec.topic),
			slog.Int("partition", int(rec.partition)),
			slog.Int64("offset", rec.offset),
		)
		return true, err
	}
	return false, nil
}

func (d *SaramaDriver) OnAck(ack *pb.ConnectorAck) {
	if ack == nil || ack.Checkpoint == nil {
		return
	}
	kafkaAck := ack.Checkpoint.GetKafka()
	if kafkaAck == nil {
		return
	}

	rec := recordID{topic: kafkaAck.Topic, partition: kafkaAck.Partition, offset: kafkaAck.Offset}
	if handled := d.ackManager.Ack(rec); !handled {
		d.logger(slog.String("stage", "ack"), slog.String("status", "missing")).Warn(
			"received ack with no pending record",
			slog.String("topic", rec.topic),
			slog.Int("partition", int(rec.partition)),
			slog.Int64("offset", rec.offset),
		)
		return
	}
	d.logger(slog.String("stage", "ack"), slog.String("status", "applied")).Debug(
		"ack handled",
		slog.String("topic", rec.topic),
		slog.Int("partition", int(rec.partition)),
		slog.Int64("offset", rec.offset),
	)
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
