package kafka

import (
	"fmt"
	"github.com/IBM/sarama"
	pb "quanta/api/proto/v1"
	"quanta/sink"
)

type Config struct {
	Brokers []string `yaml:"brokers"`
	Topic   string   `yaml:"topic"`
	Acks    int16    `yaml:"required_acks"` // 0,1,-1
}

type driver struct {
	cfg Config
	p   sarama.AsyncProducer
}

func (d *driver) Configure(c any) error {
	cfg, ok := c.(Config)
	if !ok {
		return fmt.Errorf("kafka-sink: want Config")
	}
	d.cfg = cfg

	sc := sarama.NewConfig()
	sc.Producer.RequiredAcks = sarama.RequiredAcks(cfg.Acks)
	var err error
	d.p, err = sarama.NewAsyncProducer(cfg.Brokers, sc)
	return err
}

func (d *driver) Push(f *pb.Frame) error {
	d.p.Input() <- &sarama.ProducerMessage{
		Topic: d.cfg.Topic,
		Key:   sarama.ByteEncoder(f.Key),
		Value: sarama.ByteEncoder(f.Value),
	}
	return nil
}

func (d *driver) Close() error {
	return d.p.Close()
}

func init() { sink.Register("kafka", func() sink.Adapter { return &driver{} }) }
