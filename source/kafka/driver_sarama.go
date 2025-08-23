package kafka

import (
	"context"
	"log"
	"sync"

	"github.com/IBM/sarama"
	"google.golang.org/protobuf/types/known/timestamppb"
	pb "quanta/api/proto/v1"
)

/*
RECORD IDENTITY – used as a map key for pending E2E commits.
Keeping `topic` removes collisions when the connector
subscribes to multiple topics inside the same consumer-group.
*/
type recordID struct {
	topic     string
	partition int32
	offset    int64
}

/*
SaramaDriver implements the Adapter interface with two commit modes:

 1. CommitAuto – “classic” Kafka semantics: commit right after the frame
    leaves the connector (plus local time-based throttling).

 2. CommitE2E – wait until the engine (or sink) returns a ConnectorAck
    containing this record’s CheckpointToken.
*/
type SaramaDriver struct {
	// immutable after Configure
	cfg   Config
	mode  CommitMode
	cl    sarama.Client
	group sarama.ConsumerGroup
	bp    *Controller
	cp    *Manager[struct{}]

	// protects `pending`
	mu      sync.Mutex
	pending map[recordID]func() // resolve() callbacks waiting for Ack
}

/*───────────────────────────── Lifecycle ─────────────────────────────*/

func (d *SaramaDriver) Configure(config Config) error {
	d.cfg, d.mode = config, config.CommitMode
	d.pending = make(map[recordID]func())

	d.bp = NewController(config.BackPressure.Capacity, config.BackPressure.Capacity/10, config.BackPressure.CheckInt)
	d.cp = NewManager[struct{}](config.BackPressure.Capacity, config.Checkpoint.CommitInt)

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

/*───────────────────────── Consumer-group handler ────────────────────*/

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

	// metrics/debugging: count how many we are dropping
	dropped := len(h.driver.pending)

	// purge map; cheaper than range delete
	h.driver.pending = make(map[recordID]func())

	if dropped > 0 {
		// optional: log at Info level
		// log.Printf("sarama-driver: rebalance – cleared %d pending callbacks", dropped)
	}
	return nil
}

func (h *groupHandler) ConsumeClaim(
		sess sarama.ConsumerGroupSession,
		claim sarama.ConsumerGroupClaim,
) error {

	for msg := range claim.Messages() {

		/* BLOCK if the back-pressure budget is exhausted */
		if err := h.driver.bp.Acquire(sess.Context()); err != nil {
			return err // ctx cancelled – propagate up
		}

		/* local checkpoint (time-throttled) */
		resolve, err := h.driver.cp.Track(sess.Context(), struct{}{})
		if err != nil {
			return err
		}

		/* Build CheckpointToken for this record */
		token := &pb.CheckpointToken{
			Kind: &pb.CheckpointToken_Kafka{
				Kafka: &pb.KafkaOffset{
					Topic:     msg.Topic,
					Partition: msg.Partition,
					Offset:    msg.Offset,
				},
			},
		}

		/* Emit to the engine */
		frame := &pb.Frame{
			Key:        msg.Key,
			Value:      msg.Value,
			Headers:    toHeaderMap(msg.Headers),
			Ts:         timestamppb.New(msg.Timestamp),
			Checkpoint: token,
		}
		if err := h.emit(frame); err != nil {
			return err
		}

		rec := recordID{msg.Topic, msg.Partition, msg.Offset}

		if h.driver.mode == CommitAuto {
			// ↳ Classic behavior: commit immediately (subject to cp throttle)
			_, due := resolve()
			sess.MarkMessage(msg, "")
			if due {
				sess.Commit()
			}
		} else { // CommitE2E
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
	return nil
}

/*───────────────────────────── Ack sink ─────────────────────────────*/
// OnAck is called by the Runner whenever a ConnectorAck arrives.
func (d *SaramaDriver) OnAck(ack *pb.ConnectorAck) {
	if ack == nil || ack.Checkpoint == nil {
		return
	}
	k := ack.Checkpoint.GetKafka()
	if k == nil {
		return // not a Kafka offset
	}
	rec := recordID{k.Topic, k.Partition, k.Offset}

	d.mu.Lock()
	cb, ok := d.pending[rec]
	if ok {
		delete(d.pending, rec) // free memory
	}
	d.mu.Unlock()

	if ok { // callback really exists → first time we see this token
		cb()            // resolve + maybe commit
		d.bp.Release(1) // give credit back
		// log AFTER releasing, once per real commit
		log.Printf("Ack %s[%d]@%d – token released",
			k.Topic, k.Partition, k.Offset)
	}
}

//// OnAck is called by the engine/gateway whenever a ConnectorAck arrives
//// at the bidirectional gRPC stream.
//func (d *SaramaDriver) OnAck(ack *pb.ConnectorAck) {
//	if ack == nil || ack.Checkpoint == nil {
//		return
//	}
//	kafkaToken := ack.Checkpoint.GetKafka()
//	if kafkaToken == nil {
//		return // not a Kafka token
//	}
//	log.Printf("Ack %s[%d]@%d – token released",
//		kafkaToken.Topic, kafkaToken.Partition, kafkaToken.Offset)
//	rec := recordID{kafkaToken.Topic, kafkaToken.Partition, kafkaToken.Offset}
//
//	d.mu.Lock()
//	cb, ok := d.pending[rec]
//	if ok {
//		delete(d.pending, rec) // free memory
//	}
//	d.mu.Unlock()
//
//	if ok {
//		cb()            // executes resolve() + Session commits
//		d.bp.Release(1) // ← give one token back to provider
//
//	}
//}

/*──────────────────────────── helpers ────────────────────────────*/

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
