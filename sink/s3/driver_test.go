package s3

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	s3svc "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	pb "quanta/api/proto/v1"
	qerr "quanta/internal/errors"
	"quanta/sink"
)

type spyClient struct {
	mu    sync.Mutex
	calls []spyPut
	err   error
}

type spyPut struct {
	bucket string
	key    string
	body   []byte
}

func (s *spyClient) PutObject(ctx context.Context, params *s3svc.PutObjectInput, _ ...func(*s3svc.Options)) (*s3svc.PutObjectOutput, error) {
	var body []byte
	if params.Body != nil {
		body, _ = io.ReadAll(params.Body)
	}
	s.mu.Lock()
	s.calls = append(s.calls, spyPut{
		bucket: *params.Bucket,
		key:    *params.Key,
		body:   body,
	})
	s.mu.Unlock()
	return &s3svc.PutObjectOutput{}, s.err
}

func validCfg() Config {
	return Config{
		Bucket:        "test-bucket",
		Region:        "us-east-1",
		Prefix:        "logs",
		FileSuffix:    ".jsonl",
		BatchSize:     3,
		FlushInterval: time.Hour,
		AuthStrategy:  AuthIAMRole,
		Format:        "jsonl",
	}
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name     string
		give     Config
		wantErr  bool
		wantKind qerr.Kind
	}{
		{
			name: "valid IAM config",
			give: Config{
				Bucket: "b", Region: "us-east-1",
				BatchSize: 100, FlushInterval: time.Second,
				AuthStrategy: AuthIAMRole,
			},
		},
		{
			name: "missing bucket",
			give: Config{
				Region: "us-east-1", BatchSize: 100,
				FlushInterval: time.Second, AuthStrategy: AuthIAMRole,
			},
			wantErr: true, wantKind: qerr.KindConfig,
		},
		{
			name: "invalid format",
			give: Config{
				Bucket: "b", Region: "us-east-1",
				BatchSize: 100, FlushInterval: time.Second,
				AuthStrategy: AuthIAMRole, Format: "parquet",
			},
			wantErr: true, wantKind: qerr.KindConfig,
		},
		{
			name: "format defaults to jsonl",
			give: Config{
				Bucket: "b", Region: "us-east-1",
				BatchSize: 100, FlushInterval: time.Second,
				AuthStrategy: AuthIAMRole,
			},
		},
		{
			name: "batch_size and flush_interval default when zero",
			give: Config{
				Bucket: "b", Region: "us-east-1",
				AuthStrategy: AuthIAMRole,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.give.validate()
			if !tt.wantErr {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.True(t, qerr.IsKind(err, tt.wantKind), "got: %v", err)
		})
	}
}

func TestDriverPublishAndFlush(t *testing.T) {
	defer goleak.VerifyNone(t)

	spy := &spyClient{}
	d := newTestDriver(t, spy)
	defer d.Close(context.Background())

	ctx := context.Background()

	tok1 := &pb.CheckpointToken{Kind: &pb.CheckpointToken_Kafka{Kafka: &pb.KafkaOffset{Topic: "t", Partition: 0, Offset: 1}}}
	tok2 := &pb.CheckpointToken{Kind: &pb.CheckpointToken_Kafka{Kafka: &pb.KafkaOffset{Topic: "t", Partition: 0, Offset: 2}}}
	tok3 := &pb.CheckpointToken{Kind: &pb.CheckpointToken_Kafka{Kafka: &pb.KafkaOffset{Topic: "t", Partition: 0, Offset: 3}}}

	require.NoError(t, d.Publish(ctx, &pb.Frame{Value: []byte(`{"a":1}`), Checkpoint: tok1}))
	require.NoError(t, d.Publish(ctx, &pb.Frame{Value: []byte(`{"b":2}`), Checkpoint: tok2}))

	spy.mu.Lock()
	assert.Empty(t, spy.calls, "should not flush before batch full")
	spy.mu.Unlock()

	require.NoError(t, d.Publish(ctx, &pb.Frame{Value: []byte(`{"c":3}`), Checkpoint: tok3}))

	require.Eventually(t, func() bool {
		spy.mu.Lock()
		defer spy.mu.Unlock()
		return len(spy.calls) == 1
	}, 2*time.Second, 10*time.Millisecond, "expected PutObject call after batch full")

	spy.mu.Lock()
	call := spy.calls[0]
	spy.mu.Unlock()

	assert.Equal(t, "test-bucket", call.bucket)
	assert.Contains(t, call.key, "logs/")
	assert.Contains(t, call.key, ".jsonl")

	want := "{\"a\":1}\n{\"b\":2}\n{\"c\":3}\n"
	assert.Equal(t, want, string(call.body))
}

func TestDriverAckOnFlush(t *testing.T) {
	defer goleak.VerifyNone(t)

	var acked []*pb.CheckpointToken
	var ackMu sync.Mutex
	spy := &spyClient{}
	d := newTestDriverWithOpts(t, spy, sink.BuildOptions{
		Ack: func(_ context.Context, tok *pb.CheckpointToken) {
			ackMu.Lock()
			acked = append(acked, tok)
			ackMu.Unlock()
		},
	})
	defer d.Close(context.Background())

	ctx := context.Background()
	toks := make([]*pb.CheckpointToken, 3)
	for i := range toks {
		toks[i] = &pb.CheckpointToken{Kind: &pb.CheckpointToken_Kafka{
			Kafka: &pb.KafkaOffset{Offset: int64(i)},
		}}
		require.NoError(t, d.Publish(ctx, &pb.Frame{Value: []byte("x"), Checkpoint: toks[i]}))
	}

	require.Eventually(t, func() bool {
		ackMu.Lock()
		defer ackMu.Unlock()
		return len(acked) == 3
	}, 2*time.Second, 10*time.Millisecond, "expected 3 acks")

	ackMu.Lock()
	for i, tok := range acked {
		assert.Same(t, toks[i], tok, "ack[%d] should be same pointer", i)
	}
	ackMu.Unlock()
}

func TestDriverCloseFlushesRemaining(t *testing.T) {
	defer goleak.VerifyNone(t)

	spy := &spyClient{}
	d := newTestDriver(t, spy)

	ctx := context.Background()

	require.NoError(t, d.Publish(ctx, &pb.Frame{Value: []byte("partial"), Checkpoint: &pb.CheckpointToken{}}))

	require.NoError(t, d.Close(ctx))

	spy.mu.Lock()
	require.Len(t, spy.calls, 1, "Close must flush partial batch")
	assert.Equal(t, "partial\n", string(spy.calls[0].body))
	spy.mu.Unlock()
}

func TestDriverClonesSafely(t *testing.T) {
	defer goleak.VerifyNone(t)

	spy := &spyClient{}
	d := newTestDriver(t, spy)
	defer d.Close(context.Background())

	ctx := context.Background()
	val := []byte("original")
	require.NoError(t, d.Publish(ctx, &pb.Frame{Value: val, Checkpoint: &pb.CheckpointToken{}}))

	val[0] = 'X'

	for range 2 {
		require.NoError(t, d.Publish(ctx, &pb.Frame{Value: []byte("pad"), Checkpoint: &pb.CheckpointToken{}}))
	}

	require.Eventually(t, func() bool {
		spy.mu.Lock()
		defer spy.mu.Unlock()
		return len(spy.calls) == 1
	}, 2*time.Second, 10*time.Millisecond)

	spy.mu.Lock()
	assert.True(t, bytes.Contains(spy.calls[0].body, []byte("original")), "data must be independent copy")
	spy.mu.Unlock()
}

func TestDriverImplementsInterfaces(t *testing.T) {
	var _ sink.Adapter = (*s3Driver)(nil)
	caps := (&s3Driver{}).Caps()
	assert.True(t, caps.AckAware, "s3 sink must be ack-aware")
	assert.True(t, caps.NackAware, "s3 sink must be nack-aware")
}

func TestDriverUploadError_WithholdsAck(t *testing.T) {
	defer goleak.VerifyNone(t)

	var acked atomic.Int32
	spy := &spyClient{err: errors.New("S3 unavailable")}
	d := newTestDriverWithOpts(t, spy, sink.BuildOptions{
		Ack: func(_ context.Context, _ *pb.CheckpointToken) { acked.Add(1) },
	})
	defer d.Close(context.Background())

	ctx := context.Background()
	for i := range 3 {
		tok := &pb.CheckpointToken{Kind: &pb.CheckpointToken_Kafka{
			Kafka: &pb.KafkaOffset{Offset: int64(i)},
		}}
		require.NoError(t, d.Publish(ctx, &pb.Frame{Value: []byte("x"), Checkpoint: tok}))
	}

	require.Eventually(t, func() bool {
		spy.mu.Lock()
		defer spy.mu.Unlock()
		return len(spy.calls) == 1
	}, 2*time.Second, 10*time.Millisecond, "expected PutObject call")

	assert.Equal(t, int32(0), acked.Load(), "ack must be withheld on upload failure")
}

type nackRecord struct {
	frame *pb.Frame
	err   error
}

func TestDriverNack(t *testing.T) {
	tests := []struct {
		name       string
		giveErr    error
		encFail    bool
		wantNacks  int
		wantAcks   int
		wantFrames bool
	}{
		{
			name:       "upload failure nacks all frames in batch",
			giveErr:    errors.New("S3 unavailable"),
			wantNacks:  3,
			wantAcks:   0,
			wantFrames: true,
		},
		{
			name:       "successful upload acks, no nacks",
			giveErr:    nil,
			wantNacks:  0,
			wantAcks:   3,
			wantFrames: false,
		},
		{
			name:       "encode failure nacks all frames in batch",
			encFail:    true,
			wantNacks:  3,
			wantAcks:   0,
			wantFrames: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer goleak.VerifyNone(t)

			spy := &spyClient{err: tt.giveErr}

			var enc Encoder
			if tt.encFail {
				enc = &failEncoder{}
			}

			var acked atomic.Int32
			var nackMu sync.Mutex
			var nacks []nackRecord
			d := newTestDriverWithEncoder(t, spy, enc, sink.BuildOptions{
				Ack: func(_ context.Context, _ *pb.CheckpointToken) { acked.Add(1) },
				Nack: func(_ context.Context, f *pb.Frame, err error) {
					nackMu.Lock()
					nacks = append(nacks, nackRecord{frame: f, err: err})
					nackMu.Unlock()
				},
			})
			defer d.Close(context.Background())

			ctx := context.Background()
			frames := make([]*pb.Frame, 3)
			for i := range frames {
				frames[i] = &pb.Frame{
					Key:   fmt.Appendf(nil, "key-%d", i),
					Value: fmt.Appendf(nil, `{"i":%d}`, i),
					Checkpoint: &pb.CheckpointToken{Kind: &pb.CheckpointToken_Kafka{
						Kafka: &pb.KafkaOffset{Topic: "t", Partition: 0, Offset: int64(i)},
					}},
				}
				require.NoError(t, d.Publish(ctx, frames[i]))
			}

			require.Eventually(t, func() bool {
				nackMu.Lock()
				n := len(nacks)
				nackMu.Unlock()
				a := int(acked.Load())
				return n+a >= 3
			}, 2*time.Second, 10*time.Millisecond, "expected ack or nack for all frames")

			assert.Equal(t, int32(tt.wantAcks), acked.Load(), "ack count mismatch")

			nackMu.Lock()
			assert.Len(t, nacks, tt.wantNacks, "nack count mismatch")

			if tt.wantFrames {
				for i, nr := range nacks {
					assert.Equal(t, frames[i].Checkpoint, nr.frame.Checkpoint,
						"nack[%d] checkpoint must match published frame", i)
					assert.Error(t, nr.err, "nack[%d] must carry error", i)
				}
			}
			nackMu.Unlock()
		})
	}
}

type failEncoder struct{}

func (f *failEncoder) Encode(_ [][]byte) ([]byte, error) {
	return nil, errors.New("encoder broken")
}

func (f *failEncoder) ContentType() string { return "application/octet-stream" }

func newTestDriver(t *testing.T, spy *spyClient) *s3Driver {
	return newTestDriverWithOpts(t, spy, sink.BuildOptions{})
}

func newTestDriverWithOpts(t *testing.T, spy *spyClient, opts sink.BuildOptions) *s3Driver {
	t.Helper()
	cfg := validCfg()
	require.NoError(t, cfg.validate())

	enc, err := newEncoder(cfg.Format, nil)
	require.NoError(t, err)

	return newDriverWithClient(context.Background(), cfg, spy, enc, opts)
}

func newTestDriverWithEncoder(t *testing.T, spy *spyClient, enc Encoder, opts sink.BuildOptions) *s3Driver {
	t.Helper()
	cfg := validCfg()
	require.NoError(t, cfg.validate())

	if enc == nil {
		var err error
		enc, err = newEncoder(cfg.Format, nil)
		require.NoError(t, err)
	}

	return newDriverWithClient(context.Background(), cfg, spy, enc, opts)
}
