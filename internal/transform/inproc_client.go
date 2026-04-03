package transform

import (
	"context"
	"errors"

	pb "quanta/api/proto/v1"

	"google.golang.org/grpc"
)

var ErrStreamNotSupported = errors.New("streaming not supported for in-process client")

type Transformer interface {
	Metadata(context.Context) (*pb.MetadataResponse, error)
	Health(context.Context) (*pb.HealthResponse, error)
	Transform(context.Context, *pb.TransformRequest) (*pb.TransformResponse, error)
}

type InProcessClient struct {
	impl Transformer
}

var _ Client = (*InProcessClient)(nil)

func NewInProcessClient(impl Transformer) *InProcessClient {
	return &InProcessClient{impl: impl}
}

func (c *InProcessClient) Metadata(ctx context.Context) (*pb.MetadataResponse, error) {
	return c.impl.Metadata(ctx)
}

func (c *InProcessClient) Health(ctx context.Context) (*pb.HealthResponse, error) {
	return c.impl.Health(ctx)
}

func (c *InProcessClient) Transform(ctx context.Context, req *pb.TransformRequest) (*pb.TransformResponse, error) {
	return c.impl.Transform(ctx, req)
}

func (c *InProcessClient) Stream(context.Context, ...grpc.CallOption) (pb.TransformService_TransformStreamClient, error) {
	return nil, ErrStreamNotSupported
}

func (c *InProcessClient) Close() error { return nil }
