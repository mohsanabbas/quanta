package transform

import (
	"context"
	"fmt"

	pb "quanta/api/proto/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
)

type Client interface {
	Metadata(ctx context.Context) (*pb.MetadataResponse, error)
	Health(ctx context.Context) (*pb.HealthResponse, error)
	Transform(ctx context.Context, req *pb.TransformRequest) (*pb.TransformResponse, error)
	Stream(ctx context.Context, opts ...grpc.CallOption) (pb.TransformService_TransformStreamClient, error)
	Close() error
}

type GRPCClient struct {
	conn *grpc.ClientConn
	svc  pb.TransformServiceClient
}

func NewGRPCClient(ctx context.Context, target string, opts ...grpc.DialOption) (*GRPCClient, error) {
	if len(opts) == 0 {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}
	conn, err := grpc.NewClient(target, opts...)
	if err != nil {
		return nil, err
	}
	if ctx != nil {
		conn.Connect()
		for state := conn.GetState(); state != connectivity.Ready; state = conn.GetState() {
			if !conn.WaitForStateChange(ctx, state) {
				if err := ctx.Err(); err != nil {
					_ = conn.Close()
					return nil, err
				}
				break
			}
		}
	}
	return &GRPCClient{
		conn: conn,
		svc:  pb.NewTransformServiceClient(conn),
	}, nil
}

func (c *GRPCClient) Metadata(ctx context.Context) (*pb.MetadataResponse, error) {
	return c.svc.Metadata(ctx, &pb.MetadataRequest{})
}
func (c *GRPCClient) Health(ctx context.Context) (*pb.HealthResponse, error) {
	return c.svc.Health(ctx, &pb.HealthRequest{})
}
func (c *GRPCClient) Transform(ctx context.Context, req *pb.TransformRequest) (*pb.TransformResponse, error) {
	return c.svc.Transform(ctx, req)
}
func (c *GRPCClient) Stream(ctx context.Context, opts ...grpc.CallOption) (pb.TransformService_TransformStreamClient, error) {
	return c.svc.TransformStream(ctx, opts...)
}
func (c *GRPCClient) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

type InProcessClient struct {
	impl Transformer
}
type Transformer interface {
	Metadata(context.Context) (*pb.MetadataResponse, error)
	Health(context.Context) (*pb.HealthResponse, error)
	Transform(context.Context, *pb.TransformRequest) (*pb.TransformResponse, error)
}

func NewInProcessClient(impl Transformer) *InProcessClient { return &InProcessClient{impl: impl} }
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
	return nil, fmt.Errorf("streaming not supported for in‑proc client")
}
func (c *InProcessClient) Close() error { return nil }
