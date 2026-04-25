package kafka

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	pb "quanta/api/proto/v1"
	"quanta/sink"

	"github.com/IBM/sarama"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

type fakeAsyncProducer struct {
	inputCh    chan *sarama.ProducerMessage
	successCh  chan *sarama.ProducerMessage
	errorCh    chan *sarama.ProducerError
	closeCh    chan struct{}
	closeOnce  sync.Once
	txnStatus  sarama.ProducerTxnStatusFlag
	isTransact bool
}

func newFakeAsyncProducer() *fakeAsyncProducer {
	return &fakeAsyncProducer{
		inputCh:   make(chan *sarama.ProducerMessage, 16),
		successCh: make(chan *sarama.ProducerMessage, 16),
		errorCh:   make(chan *sarama.ProducerError, 16),
		closeCh:   make(chan struct{}),
	}
}

func (f *fakeAsyncProducer) Input() chan<- *sarama.ProducerMessage     { return f.inputCh }
func (f *fakeAsyncProducer) Successes() <-chan *sarama.ProducerMessage { return f.successCh }
func (f *fakeAsyncProducer) Errors() <-chan *sarama.ProducerError      { return f.errorCh }
func (f *fakeAsyncProducer) IsTransactional() bool                     { return f.isTransact }
func (f *fakeAsyncProducer) TxnStatus() sarama.ProducerTxnStatusFlag   { return f.txnStatus }
func (f *fakeAsyncProducer) BeginTxn() error                           { return nil }
func (f *fakeAsyncProducer) CommitTxn() error                          { return nil }
func (f *fakeAsyncProducer) AbortTxn() error                           { return nil }
func (f *fakeAsyncProducer) AddOffsetsToTxn(map[string][]*sarama.PartitionOffsetMetadata, string) error {
	return nil
}

func (f *fakeAsyncProducer) AddMessageToTxn(*sarama.ConsumerMessage, string, *string) error {
	return nil
}

func (f *fakeAsyncProducer) Close() error {
	f.AsyncClose()
	return nil
}

func (f *fakeAsyncProducer) AsyncClose() {
	f.closeOnce.Do(func() {
		close(f.successCh)
		close(f.errorCh)
		close(f.closeCh)
	})
}

func newTestSink(fp *fakeAsyncProducer, opts sink.BuildOptions) *saramaSink {
	return newSaramaSinkWithProducer(context.Background(), Config{Topic: "test-topic"}, fp, opts)
}

func kafkaCheckpoint(topic string, partition int32, offset int64) *pb.CheckpointToken {
	return &pb.CheckpointToken{
		Kind: &pb.CheckpointToken_Kafka{
			Kafka: &pb.KafkaOffset{
				Topic:     topic,
				Partition: partition,
				Offset:    offset,
			},
		},
	}
}

func testFrame(key, value string, tok *pb.CheckpointToken) *pb.Frame {
	return &pb.Frame{
		Key:        []byte(key),
		Value:      []byte(value),
		Checkpoint: tok,
	}
}

func TestSaramaSink_CompileTimeChecks(t *testing.T) {
	defer goleak.VerifyNone(t)
	var _ sink.Adapter = (*saramaSink)(nil)
}

func TestSaramaSink_Caps(t *testing.T) {
	t.Parallel()
	s := &saramaSink{}
	caps := s.Caps()
	assert.True(t, caps.AckAware, "kafka sink must be ack-aware")
	assert.True(t, caps.NackAware, "kafka sink must be nack-aware")
}

func TestSaramaSink_Publish_Enqueues(t *testing.T) {
	defer goleak.VerifyNone(t)
	fp := newFakeAsyncProducer()
	s := newTestSink(fp, sink.BuildOptions{})
	defer func() {
		fp.AsyncClose()
		<-s.doneCh
	}()

	tok := kafkaCheckpoint("src", 0, 42)
	err := s.Publish(context.Background(), testFrame("k", "v", tok))
	require.NoError(t, err)

	msg := <-fp.inputCh
	assert.Equal(t, "test-topic", msg.Topic)
	inf, ok := msg.Metadata.(*inflight)
	require.True(t, ok)
	assert.Equal(t, tok, inf.frame.Checkpoint)
}

func TestSaramaSink_Publish_ContextCancelled(t *testing.T) {
	defer goleak.VerifyNone(t)

	fp := newFakeAsyncProducer()
	fp.inputCh = make(chan *sarama.ProducerMessage)
	s := newTestSink(fp, sink.BuildOptions{})
	defer func() {
		fp.AsyncClose()
		<-s.doneCh
	}()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := s.Publish(ctx, testFrame("k", "v", kafkaCheckpoint("src", 0, 1)))
	assert.ErrorIs(t, err, context.Canceled)
}

func TestSaramaSink_Publish_NoTopic(t *testing.T) {
	defer goleak.VerifyNone(t)
	fp := newFakeAsyncProducer()
	s := newSaramaSinkWithProducer(context.Background(), Config{}, fp, sink.BuildOptions{})
	defer func() {
		fp.AsyncClose()
		<-s.doneCh
	}()

	err := s.Publish(context.Background(), testFrame("k", "v", nil))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no topic resolved")
}

func TestSaramaSink_AckLoopAcksOnSuccess(t *testing.T) {
	defer goleak.VerifyNone(t)

	fp := newFakeAsyncProducer()

	var acked atomic.Int32
	var ackedTok atomic.Value
	s := newTestSink(fp, sink.BuildOptions{
		Ack: func(_ context.Context, tok *pb.CheckpointToken) {
			acked.Add(1)
			ackedTok.Store(tok)
		},
	})

	tok := kafkaCheckpoint("src", 0, 100)
	err := s.Publish(context.Background(), testFrame("k", "v", tok))
	require.NoError(t, err)

	msg := <-fp.inputCh
	fp.successCh <- msg

	fp.AsyncClose()
	<-s.doneCh

	assert.Equal(t, int32(1), acked.Load())
	assert.Equal(t, tok, ackedTok.Load())
}

func TestSaramaSink_AckLoopWithholdsAckOnError(t *testing.T) {
	defer goleak.VerifyNone(t)

	fp := newFakeAsyncProducer()

	var acked atomic.Int32
	s := newTestSink(fp, sink.BuildOptions{
		Ack: func(_ context.Context, _ *pb.CheckpointToken) { acked.Add(1) },
	})

	tok := kafkaCheckpoint("src", 0, 200)
	err := s.Publish(context.Background(), testFrame("k", "v", tok))
	require.NoError(t, err)

	msg := <-fp.inputCh
	fp.errorCh <- &sarama.ProducerError{
		Msg: msg,
		Err: sarama.ErrOutOfBrokers,
	}

	fp.AsyncClose()
	<-s.doneCh

	assert.Equal(t, int32(0), acked.Load())
}

func TestSaramaSink_AckLoopDrainsAllInFlight(t *testing.T) {
	defer goleak.VerifyNone(t)

	fp := newFakeAsyncProducer()

	var acked atomic.Int32
	s := newTestSink(fp, sink.BuildOptions{
		Ack: func(_ context.Context, _ *pb.CheckpointToken) { acked.Add(1) },
	})

	const n = 5
	for i := range n {
		tok := kafkaCheckpoint("src", 0, int64(i))
		require.NoError(t, s.Publish(context.Background(), testFrame("k", "v", tok)))
	}

	for i := range n {
		msg := <-fp.inputCh
		if i%2 == 0 {
			fp.successCh <- msg
		} else {
			fp.errorCh <- &sarama.ProducerError{Msg: msg, Err: sarama.ErrOutOfBrokers}
		}
	}

	fp.AsyncClose()
	<-s.doneCh

	assert.Equal(t, int32(3), acked.Load())
}

func TestSaramaSink_Close_WaitsAckLoop(t *testing.T) {
	defer goleak.VerifyNone(t)

	fp := newFakeAsyncProducer()
	s := newTestSink(fp, sink.BuildOptions{})

	err := s.Close(context.Background())
	require.NoError(t, err)
	assert.Nil(t, s.prod)
}

func TestSaramaSink_HeaderTopicOverride(t *testing.T) {
	defer goleak.VerifyNone(t)

	fp := newFakeAsyncProducer()
	cfg := Config{Topic: "default-topic", HeaderTopicKey: "X-Target"}
	s := newSaramaSinkWithProducer(context.Background(), cfg, fp, sink.BuildOptions{})
	defer func() {
		fp.AsyncClose()
		<-s.doneCh
	}()

	f := &pb.Frame{
		Key:   []byte("k"),
		Value: []byte("v"),
		Headers: map[string][]byte{
			"X-Target": []byte("override-topic"),
		},
	}

	require.NoError(t, s.Publish(context.Background(), f))
	msg := <-fp.inputCh
	assert.Equal(t, "override-topic", msg.Topic)
}

func TestSaramaSink_AckLoopNacksOnError(t *testing.T) {
	defer goleak.VerifyNone(t)

	fp := newFakeAsyncProducer()

	var nacked atomic.Int32
	var nackedFrame atomic.Value
	s := newTestSink(fp, sink.BuildOptions{
		Nack: func(_ context.Context, f *pb.Frame, _ error) {
			nacked.Add(1)
			nackedFrame.Store(f)
		},
	})

	frame := &pb.Frame{
		Key:   []byte("k"),
		Value: []byte("nack-me"),
		Checkpoint: &pb.CheckpointToken{Kind: &pb.CheckpointToken_Kafka{
			Kafka: &pb.KafkaOffset{Topic: "t", Partition: 0, Offset: 42},
		}},
	}

	require.NoError(t, s.Publish(context.Background(), frame))
	msg := <-fp.inputCh

	fp.errorCh <- &sarama.ProducerError{
		Msg: msg,
		Err: errors.New("broker down"),
	}

	fp.AsyncClose()
	<-s.doneCh

	assert.Equal(t, int32(1), nacked.Load())
	gotFrame, ok := nackedFrame.Load().(*pb.Frame)
	require.True(t, ok)
	assert.Equal(t, frame.Checkpoint, gotFrame.Checkpoint)
}
