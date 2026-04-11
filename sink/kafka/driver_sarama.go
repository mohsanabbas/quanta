package kafka

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"time"

	pb "quanta/api/proto/v1"
	qerr "quanta/internal/errors"
	"quanta/sink"

	"github.com/IBM/sarama"
)

type SaramaSink struct {
	cfg    Config
	prod   sarama.AsyncProducer
	ack    sink.EmitFn
	nack   sink.NackFn
	doneCh chan struct{}
}

var (
	_ sink.Adapter   = (*SaramaSink)(nil)
	_ sink.AckAware  = (*SaramaSink)(nil)
	_ sink.NackAware = (*SaramaSink)(nil)
)

type inflight struct {
	frame *pb.Frame
}

func (s *SaramaSink) Configure(ctx context.Context, raw any) error {
	var cfg Config
	switch v := raw.(type) {
	case Config:
		cfg = v
	case *Config:
		if v != nil {
			cfg = *v
		}
	default:
		return qerr.Sink("kafka", "configure", errors.New("invalid config type"))
	}
	if err := cfg.validateAndDefault(); err != nil {
		return err
	}
	s.cfg = cfg

	sc, err := buildSaramaConfig(cfg)
	if err != nil {
		return err
	}

	prod, err := sarama.NewAsyncProducer(cfg.Brokers, sc)
	if err != nil {
		return qerr.Sink("kafka", "connect", err)
	}
	s.prod = prod
	s.doneCh = make(chan struct{})
	go s.ackLoop(context.WithoutCancel(ctx))
	return nil
}

func buildSaramaConfig(cfg Config) (*sarama.Config, error) {
	ver, err := sarama.ParseKafkaVersion(cfg.Version)
	if err != nil {
		return nil, qerr.Config("kafka-sink", "parse-version", err)
	}
	sc := sarama.NewConfig()
	sc.Version = ver
	if cfg.ClientID != "" {
		sc.ClientID = cfg.ClientID
	}
	sc.Producer.Return.Successes = true
	sc.Producer.Return.Errors = true

	switch cfg.Acks {
	case _acksNone:
		sc.Producer.RequiredAcks = sarama.NoResponse
	case _acksLocal:
		sc.Producer.RequiredAcks = sarama.WaitForLocal
	case _acksAll:
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

	return sc, nil
}

func (s *SaramaSink) BindAck(fn sink.EmitFn) {
	s.ack = fn
}

func (s *SaramaSink) BindNack(fn sink.NackFn) {
	s.nack = fn
}

func (s *SaramaSink) Publish(ctx context.Context, f *pb.Frame) error {
	if s.prod == nil {
		return qerr.Sink("kafka", "publish", errors.New("not configured"))
	}
	topic := s.cfg.Topic
	if s.cfg.HeaderTopicKey != "" && f.Headers != nil {
		if v, ok := f.Headers[s.cfg.HeaderTopicKey]; ok && len(v) > 0 {
			topic = string(v)
		}
	}
	if topic == "" {
		return qerr.Sink("kafka", "publish", errors.New("no topic resolved"))
	}
	msg := &sarama.ProducerMessage{
		Topic:     topic,
		Key:       sarama.ByteEncoder(bytes.Clone(f.GetKey())),
		Value:     sarama.ByteEncoder(bytes.Clone(f.GetValue())),
		Timestamp: tsOrNow(f),
		Headers:   toRecordHeaders(f.GetHeaders()),
		Metadata:  &inflight{frame: f},
	}

	select {
	case s.prod.Input() <- msg:
		return nil
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

func (s *SaramaSink) ackLoop(ctx context.Context) {
	defer close(s.doneCh)
	for {
		select {
		case pm, ok := <-s.prod.Successes():
			if !ok {
				s.flushErrors(ctx)
				return
			}
			s.ackFromMetadata(ctx, pm.Metadata)
		case pe, ok := <-s.prod.Errors():
			if !ok {
				s.flushSuccesses(ctx)
				return
			}
			s.nackFromMetadata(ctx, pe)
		}
	}
}

func (s *SaramaSink) ackFromMetadata(ctx context.Context, meta any) {
	if inf, _ := meta.(*inflight); inf != nil && inf.frame != nil && s.ack != nil {
		s.ack(ctx, inf.frame.Checkpoint)
	}
}

func (s *SaramaSink) nackFromMetadata(ctx context.Context, pe *sarama.ProducerError) {
	if pe == nil || pe.Msg == nil {
		return
	}
	inf, _ := pe.Msg.Metadata.(*inflight)
	if inf == nil || inf.frame == nil {
		return
	}
	if s.nack != nil {
		s.nack(ctx, inf.frame, pe.Err)
		return
	}
	slog.Error("kafka-sink: produce failed, withholding ack for redelivery",
		"topic", pe.Msg.Topic,
		"err", pe.Err,
	)
}

func (s *SaramaSink) flushErrors(ctx context.Context) {
	for {
		select {
		case pe, ok := <-s.prod.Errors():
			if !ok {
				return
			}
			s.nackFromMetadata(ctx, pe)
		default:
			return
		}
	}
}

func (s *SaramaSink) flushSuccesses(ctx context.Context) {
	for {
		select {
		case pm, ok := <-s.prod.Successes():
			if !ok {
				return
			}
			s.ackFromMetadata(ctx, pm.Metadata)
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
		out = append(out, sarama.RecordHeader{Key: []byte(k), Value: v})
	}
	return out
}
