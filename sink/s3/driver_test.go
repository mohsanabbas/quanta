package s3

import (
	"bytes"
	"context"
	"io"
	"sync"
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

	spy := &spyClient{}
	d := newTestDriver(t, spy)
	defer d.Close(context.Background())

	var acked []*pb.CheckpointToken
	var ackMu sync.Mutex
	d.BindAck(func(tok *pb.CheckpointToken) {
		ackMu.Lock()
		acked = append(acked, tok)
		ackMu.Unlock()
	})

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
	var _ sink.Adapter = (*Driver)(nil)
	var _ sink.AckAware = (*Driver)(nil)
}

func newTestDriver(t *testing.T, spy *spyClient) *Driver {
	t.Helper()
	cfg := validCfg()
	require.NoError(t, cfg.validate())

	enc, err := newEncoder(cfg.Format)
	require.NoError(t, err)

	pool := newBatchPool(cfg.BatchSize)

	d := &Driver{
		cfg:     cfg,
		client:  spy,
		encoder: enc,
		pool:    pool,
		current: pool.Get().(*batch),
		sealCh:  make(chan *batch, cfg.BatchSize),
		stopCh:  make(chan struct{}),
		doneCh:  make(chan struct{}),
	}
	go d.flushLoop()
	return d
}
