package kafka

import (
	"fmt"
	"sync"

	pb "quanta/api/proto/v1"
	"quanta/internal/logging"
	"quanta/sink"

	"github.com/IBM/sarama"
)

type Config struct {
	Brokers []string `yaml:"brokers"`
	Topic   string   `yaml:"topic"`
	Acks    int16    `yaml:"required_acks"`
}

type driver struct {
	cfg  Config
	p    sarama.AsyncProducer
	ack  sink.EmitFn
	once sync.Once
}

func (d *driver) Configure(c any) error {
	cfg, ok := c.(Config)
	if !ok {
		return fmt.Errorf("kafka-sink: want Config")
	}
	d.cfg = cfg
	d.defaults()
	if err := d.validate(); err != nil {
		return err
	}

	sc := sarama.NewConfig()
	sc.Producer.RequiredAcks = sarama.RequiredAcks(d.cfg.Acks)
	sc.Producer.Return.Successes = true
	sc.Producer.Return.Errors = true
	var err error
	d.p, err = sarama.NewAsyncProducer(d.cfg.Brokers, sc)
	if err != nil {
		return err
	}
	d.start()
	return nil
}

func (d *driver) start() {
	d.once.Do(func() {
		go d.dispatch()
	})
}

func (d *driver) dispatch() {
	for {
		select {
		case msg, ok := <-d.p.Successes():
			if !ok {
				return
			}
			if tok, ok := msg.Metadata.(*pb.CheckpointToken); ok && d.ack != nil {
				d.ack(tok)
			}
		case err, ok := <-d.p.Errors():
			if !ok {
				return
			}
			if err != nil {
				logging.L().Error("kafka sink publish failed", "topic", d.cfg.Topic, "err", err)
			}
		}
	}
}

func (d *driver) defaults() {
	if d.cfg.Acks == 0 {
		d.cfg.Acks = int16(sarama.WaitForLocal)
	}
}

func (d *driver) validate() error {
	if len(d.cfg.Brokers) == 0 {
		return fmt.Errorf("kafka-sink: brokers required")
	}
	if d.cfg.Topic == "" {
		return fmt.Errorf("kafka-sink: topic required")
	}
	return nil
}

func (d *driver) Push(f *pb.Frame) error {
	msg := &sarama.ProducerMessage{
		Topic:    d.cfg.Topic,
		Key:      sarama.ByteEncoder(f.Key),
		Value:    sarama.ByteEncoder(f.Value),
		Metadata: f.Checkpoint,
	}
	d.p.Input() <- msg
	return nil
}

func (d *driver) Close() error {
	return d.p.Close()
}

func (d *driver) BindAck(fn sink.EmitFn) {
	d.ack = fn
}

func init() {
	sink.Register("kafka", func() sink.Adapter { return &driver{} })
}
