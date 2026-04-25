// Package kafka — Sarama-backed Kafka sink driver.
//
// The driver is constructed via the package init() registration and reached
// by the runner through sink.Build("kafka", cfg, opts). Construction acquires
// the AsyncProducer and starts the ack-loop goroutine; on any failure the
// constructor releases what it acquired and returns an error. There is no
// valid "constructed but not configured" state.
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

type saramaSink struct {
	cfg    Config
	prod   sarama.AsyncProducer
	ack    sink.EmitFn
	nack   sink.NackFn
	doneCh chan struct{}
}

var _ sink.Adapter = (*saramaSink)(nil)

type inflight struct {
	frame *pb.Frame
}

func (s *saramaSink) Name() string { return "kafka" }

func (s *saramaSink) Caps() sink.Capabilities {
	return sink.Capabilities{AckAware: true, NackAware: true}
}

func newSaramaSink(ctx context.Context, cfg Config, opts sink.BuildOptions) (sink.Adapter, error) {
	if err := cfg.validateAndDefault(); err != nil {
		return nil, err
	}
	sc, err := buildSaramaConfig(cfg)
	if err != nil {
		return nil, err
	}
	prod, err := sarama.NewAsyncProducer(cfg.Brokers, sc)
	if err != nil {
		return nil, qerr.Sink("kafka", "connect", err)
	}
	return newSaramaSinkWithProducer(ctx, cfg, prod, opts), nil
}

func newSaramaSinkWithProducer(ctx context.Context, cfg Config, prod sarama.AsyncProducer, opts sink.BuildOptions) *saramaSink {
	s := &saramaSink{
		cfg:    cfg,
		prod:   prod,
		ack:    opts.Ack,
		nack:   opts.Nack,
		doneCh: make(chan struct{}),
	}
	go s.ackLoop(context.WithoutCancel(ctx))
	return s
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
	case AcksNone:
		sc.Producer.RequiredAcks = sarama.NoResponse
	case AcksLocal:
		sc.Producer.RequiredAcks = sarama.WaitForLocal
	case AcksAll:
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
	case CompressionNone:
		sc.Producer.Compression = sarama.CompressionNone
	case CompressionGZIP:
		sc.Producer.Compression = sarama.CompressionGZIP
	case CompressionSnappy:
		sc.Producer.Compression = sarama.CompressionSnappy
	case CompressionLZ4:
		sc.Producer.Compression = sarama.CompressionLZ4
	case CompressionZSTD:
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

func (s *saramaSink) Publish(ctx context.Context, f *pb.Frame) error {
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

func (s *saramaSink) Close(_ context.Context) error {
	if s.prod != nil {
		s.prod.AsyncClose()
		<-s.doneCh
		s.prod = nil
	}
	return nil
}

func (s *saramaSink) ackLoop(ctx context.Context) {
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

func (s *saramaSink) ackFromMetadata(ctx context.Context, meta any) {
	if inf, _ := meta.(*inflight); inf != nil && inf.frame != nil && s.ack != nil {
		s.ack(ctx, inf.frame.Checkpoint)
	}
}

func (s *saramaSink) nackFromMetadata(ctx context.Context, pe *sarama.ProducerError) {
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

func (s *saramaSink) flushErrors(ctx context.Context) {
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

func (s *saramaSink) flushSuccesses(ctx context.Context) {
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
