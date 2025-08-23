package kafka

import (
	"context"
	"sync"

	pb "quanta/api/proto/v1"
	"quanta/internal/logging"

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
	bp    *Controller
	cp    *Manager[struct{}]

	mu      sync.Mutex
	pending map[recordID]func()

	ackCh chan recordID
}

func (d *SaramaDriver) Configure(config Config) error {
	d.cfg, d.mode = config, config.CommitMode
	d.pending = make(map[recordID]func())

	d.bp = NewController(config.BackPressure.Capacity, config.BackPressure.Capacity/10, config.BackPressure.CheckInt)
	d.cp = NewManager[struct{}](config.BackPressure.Capacity, config.Checkpoint.CommitInt)

	d.ackCh = make(chan recordID, int(config.BackPressure.Capacity))

	ver, err := sarama.ParseKafkaVersion(config.Version)
	if err != nil {
		return err
	}
	sc := sarama.NewConfig()
	sc.Version = ver
	sc.Consumer.Return.Errors = true
	if config.TLSEn {
		sc.Net.TLS.Enable = true
	}
	if config.SASLUser != "" {
		sc.Net.SASL.Enable = true
		sc.Net.SASL.User, sc.Net.SASL.Password = config.SASLUser, config.SASLPass
	}
	switch config.StartFrom {
	case "oldest":
		sc.Consumer.Offsets.Initial = sarama.OffsetOldest
	default:
		sc.Consumer.Offsets.Initial = sarama.OffsetNewest
	}

	if d.cl, err = sarama.NewClient(config.Brokers, sc); err != nil {
		return err
	}
	d.group, err = sarama.NewConsumerGroupFromClient(config.GroupID, d.cl)
	return err
}

func (d *SaramaDriver) Run(ctx context.Context, emit EmitFunc) error {
	handler := &groupHandler{driver: d, emit: emit}

	for {
		if err := d.group.Consume(ctx, d.cfg.Topics, handler); err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
}

func (d *SaramaDriver) Close() error {
	_ = d.group.Close()
	_ = d.cl.Close()
	d.bp.Close()
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
	h.driver.mu.Lock()
	defer h.driver.mu.Unlock()

	dropped := len(h.driver.pending)

	h.driver.pending = make(map[recordID]func())

	if dropped > 0 {
		logging.L().Info("sarama-driver: rebalance – cleared pending callbacks", "count", dropped)
	}
	return nil
}

func (h *groupHandler) ConsumeClaim(
	sess sarama.ConsumerGroupSession,
	claim sarama.ConsumerGroupClaim,
) error {
	for {

		if !h.driver.bp.TryAcquire(1) {

			select {
			case rec := <-h.driver.ackCh:

				h.driver.mu.Lock()
				cb, ok := h.driver.pending[rec]
				if ok {
					delete(h.driver.pending, rec)
				}
				h.driver.mu.Unlock()
				if ok {
					cb()
					h.driver.bp.Release(1)
					logging.L().Info("kafka ack released", "topic", rec.topic, "partition", rec.partition, "offset", rec.offset)
				}
				continue
			case <-sess.Context().Done():
				return sess.Context().Err()
			}
		}

		select {
		case <-sess.Context().Done():

			h.driver.bp.Release(1)
			return sess.Context().Err()

		case rec := <-h.driver.ackCh:

			h.driver.mu.Lock()
			cb, ok := h.driver.pending[rec]
			if ok {
				delete(h.driver.pending, rec)
			}
			h.driver.mu.Unlock()
			if ok {
				cb()
				h.driver.bp.Release(1)
				logging.L().Info("kafka ack released", "topic", rec.topic, "partition", rec.partition, "offset", rec.offset)
			}

			continue

		case msg, ok := <-claim.Messages():
			if !ok {

				h.driver.bp.Release(1)
				return nil
			}

			resolve, err := h.driver.cp.Track(sess.Context(), struct{}{})
			if err != nil {

				h.driver.bp.Release(1)
				return err
			}

			token := &pb.CheckpointToken{
				Kind: &pb.CheckpointToken_Kafka{
					Kafka: &pb.KafkaOffset{Topic: msg.Topic, Partition: msg.Partition, Offset: msg.Offset},
				},
			}
			frame := &pb.Frame{Key: msg.Key, Value: msg.Value, Headers: toHeaderMap(msg.Headers), Ts: timestamppb.New(msg.Timestamp), Checkpoint: token}
			if err := h.emit(frame); err != nil {

				h.driver.bp.Release(1)
				return err
			}

			rec := recordID{msg.Topic, msg.Partition, msg.Offset}
			if h.driver.mode == CommitAuto {

				_, due := resolve()
				sess.MarkMessage(msg, "")
				if due {
					sess.Commit()
				}

				h.driver.bp.Release(1)
			} else {

				h.driver.mu.Lock()
				h.driver.pending[rec] = func() {
					_, due := resolve()
					sess.MarkMessage(msg, "")
					if due {
						sess.Commit()
					}
				}
				h.driver.mu.Unlock()
			}
		}
	}
}

func (d *SaramaDriver) OnAck(ack *pb.ConnectorAck) {
	if ack == nil || ack.Checkpoint == nil {
		return
	}
	k := ack.Checkpoint.GetKafka()
	if k == nil {
		return
	}
	rec := recordID{k.Topic, k.Partition, k.Offset}

	select {
	case d.ackCh <- rec:
	default:

		select {
		case <-d.ackCh:
		default:
		}
		select {
		case d.ackCh <- rec:
		default:

			logging.L().Warn("sarama-driver: ack channel full; dropping ack", "topic", rec.topic, "partition", rec.partition, "offset", rec.offset)
		}
	}
}

func toHeaderMap(src []*sarama.RecordHeader) map[string][]byte {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string][]byte, len(src))
	for _, h := range src {
		out[string(h.Key)] = h.Value
	}
	return out
}
