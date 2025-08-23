// --------------------------------------------------------------------------------
// driver_confluent.go – librdkafka wrapper
// --------------------------------------------------------------------------------
//go:build confluent
// +build confluent

package kafka

//
//import (
//	"context"
//
//	ck "github.com/confluentinc/confluent-kafka-go/kafka"
//	pb "github.com/yourorg/quanta/api/proto/v1"
//)
//
//type confluentAdapter struct {
//	cfg  Config
//	cons *ck.Consumer
//	bp   *controller
//	cp   *Manager
//}
//
//func init() { Register("confluent", func() Adapter { return &confluentAdapter{} }) }
//
//func (a *confluentAdapter) Configure(cfg Config) error {
//	a.cfg = cfg
//	conf := &ck.ConfigMap{
//		"bootstrap.servers":  cfg.Brokers,
//		"group.id":           cfg.GroupID,
//		"enable.auto.commit": false,
//	}
//	consumer, err := ck.NewConsumer(conf)
//	if err != nil {
//		return err
//	}
//	if err := consumer.SubscribeTopics(cfg.Topics, nil); err != nil {
//		return err
//	}
//	a.cons = consumer
//	a.bp = newController(cfg.BackPressure)
//	a.cp = newManager(cfg.Checkpoint, newConfluentStore(consumer))
//	return nil
//}
//
//func (a *confluentAdapter) Run(ctx context.Context, emit EmitFunc) error {
//	for {
//		ev := a.cons.Poll(100)
//		if ev == nil {
//			continue
//		}
//		switch v := ev.(type) {
//		case *ck.Message:
//			a.bp.Acquire(1)
//			frame := &pb.Frame{
//				Topic:     *v.TopicPartition.Topic,
//				Partition: v.TopicPartition.Partition,
//				Offset:    int64(v.TopicPartition.Offset),
//				Key:       v.Key,
//				Value:     v.Value,
//				Timestamp: v.Timestamp.UnixNano(),
//			}
//			if err := emit(frame); err != nil {
//				a.bp.Release(1)
//				return err
//			}
//			a.cons.StoreMessage(v) // async commit later
//			a.bp.Release(1)
//		case ck.Error:
//			return v
//		}
//		if ctx.Err() != nil {
//			return ctx.Err()
//		}
//	}
//}
//
//func (a *confluentAdapter) Close() error { return a.cons.Close() }
//
//type confluentStore struct{ c *ck.Consumer }
//
//func newConfluentStore(c *ck.Consumer) Store { return &confluentStore{c} }
//
//func (s *confluentStore) Load(ctx context.Context, t string, p int32) (int64, error) { return -1, nil }
//func (s *confluentStore) Commit(ctx context.Context, t string, p int32, off int64) error {
//	return s.c.Commit()
//}
