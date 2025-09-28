package kafka

import (
	"bytes"
	"context"
	"errors"
	"time"

	pb "quanta/api/proto/v1"

	"github.com/IBM/sarama"
)

type SaramaSink struct {
	cfg    Config
	prod   sarama.AsyncProducer
	doneCh chan struct{}
}

type delivery struct{ ch chan error }

func (s *SaramaSink) Configure(_ context.Context, raw any) error {
	// accept both Config and *Config
	var cfg Config
	switch v := raw.(type) {
	case Config:
		cfg = v
	case *Config:
		if v != nil {
			cfg = *v
		}
	default:
		return errors.New("kafka-sink: invalid config type")
	}
	if err := cfg.validateAndDefault(); err != nil {
		return err
	}
	s.cfg = cfg

	ver, err := sarama.ParseKafkaVersion(cfg.Version)
	if err != nil {
		return err
	}
	sc := sarama.NewConfig()
	sc.Version = ver
	if cfg.ClientID != "" {
		sc.ClientID = cfg.ClientID
	}
	sc.Producer.Return.Successes = true
	sc.Producer.Return.Errors = true

	switch cfg.Acks {
	case "none":
		sc.Producer.RequiredAcks = sarama.NoResponse
	case "local":
		sc.Producer.RequiredAcks = sarama.WaitForLocal
	case "all":
		sc.Producer.RequiredAcks = sarama.WaitForAll
	}

	sc.Producer.Idempotent = cfg.Idempotent
	if cfg.Idempotent && sc.Net.MaxOpenRequests != 1 {
		sc.Net.MaxOpenRequests = 1
	}
	sc.Producer.Retry.Max = cfg.RetryMax
	sc.Producer.Retry.Backoff = cfg.RetryBackoffMin
	sc.Producer.Retry.BackoffFunc = func(retries, _ int) time.Duration {
		d := time.Duration(retries) * cfg.RetryBackoffMin
		if d > cfg.RetryBackoffMax {
			return cfg.RetryBackoffMax
		}
		return d
	}
	sc.Producer.Timeout = cfg.Timeout

	switch cfg.Compression {
	case "none":
		sc.Producer.Compression = sarama.CompressionNone
	case "gzip":
		sc.Producer.Compression = sarama.CompressionGZIP
	case "snappy":
		sc.Producer.Compression = sarama.CompressionSnappy
	case "lz4":
		sc.Producer.Compression = sarama.CompressionLZ4
	case "zstd":
		sc.Producer.Compression = sarama.CompressionZSTD
	}

	if cfg.TLSEn {
		sc.Net.TLS.Enable = true
	}
	if cfg.SASLUser != "" {
		sc.Net.SASL.Enable = true
		sc.Net.SASL.User = cfg.SASLUser
		sc.Net.SASL.Password = cfg.SASLPass
	}

	prod, err := sarama.NewAsyncProducer(cfg.Brokers, sc)
	if err != nil {
		return err
	}
	s.prod = prod
	s.doneCh = make(chan struct{})
	go s.pump()
	return nil
}

func (s *SaramaSink) Publish(ctx context.Context, f *pb.Frame) error {
	if s.prod == nil {
		return errors.New("kafka sink not configured")
	}
	topic := s.cfg.Topic
	if s.cfg.HeaderTopicKey != "" && f.Headers != nil {
		if v, ok := f.Headers[s.cfg.HeaderTopicKey]; ok && len(v) > 0 {
			topic = string(v)
		}
	}
	if topic == "" {
		return errors.New("kafka sink: no topic resolved")
	}
	msg := &sarama.ProducerMessage{
		Topic:     topic,
		Key:       sarama.ByteEncoder(bytes.Clone(f.GetKey())),
		Value:     sarama.ByteEncoder(bytes.Clone(f.GetValue())),
		Timestamp: tsOrNow(f),
		Headers:   toRecordHeaders(f.GetHeaders()),
	}
	d := &delivery{ch: make(chan error, 1)}
	msg.Metadata = d

	select {
	case s.prod.Input() <- msg:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-d.ch:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *SaramaSink) Close(_ context.Context) error {
	if s.prod != nil {
		s.prod.AsyncClose()
		<-s.doneCh
		s.prod = nil
	}
	return nil
}

func (s *SaramaSink) pump() {
	defer close(s.doneCh)
	for {
		select {
		case pm, ok := <-s.prod.Successes():
			if !ok {
				s.flushErrors()
				return
			}
			if d, _ := pm.Metadata.(*delivery); d != nil {
				select {
				case d.ch <- nil:
				default:
				}
			}
		case pe, ok := <-s.prod.Errors():
			if !ok {
				s.flushSuccesses()
				return
			}
			if pe != nil {
				if d, _ := pe.Msg.Metadata.(*delivery); d != nil {
					select {
					case d.ch <- pe.Err:
					default:
					}
				}
			}
		}
	}
}

func (s *SaramaSink) flushErrors() {
	for {
		select {
		case pe, ok := <-s.prod.Errors():
			if !ok {
				return
			}
			if pe != nil {
				if d, _ := pe.Msg.Metadata.(*delivery); d != nil {
					select {
					case d.ch <- pe.Err:
					default:
					}
				}
			}
		default:
			return
		}
	}
}

func (s *SaramaSink) flushSuccesses() {
	for {
		select {
		case pm, ok := <-s.prod.Successes():
			if !ok {
				return
			}
			if d, _ := pm.Metadata.(*delivery); d != nil {
				select {
				case d.ch <- nil:
				default:
				}
			}
		default:
			return
		}
	}
}

func tsOrNow(f *pb.Frame) time.Time {
	if f.GetTs() != nil {
		if t := f.GetTs().AsTime(); !t.IsZero() {
			return t
		}
	}
	return time.Now()
}

func toRecordHeaders(h map[string][]byte) []sarama.RecordHeader {
	if len(h) == 0 {
		return nil
	}
	out := make([]sarama.RecordHeader, 0, len(h))
	for k, v := range h {
		// sarama wants []byte we already have it
		out = append(out, sarama.RecordHeader{Key: []byte(k), Value: v})
	}
	return out
}
