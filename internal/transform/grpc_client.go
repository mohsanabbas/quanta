package transform

import (
	"context"

	pb "quanta/api/proto/v1"
	qerr "quanta/internal/errors"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
)

type GRPCClient struct {
	conn *grpc.ClientConn
	svc  pb.TransformServiceClient
}

var _ Client = (*GRPCClient)(nil)

func NewGRPCClient(ctx context.Context, target string, opts ...grpc.DialOption) (*GRPCClient, error) {
	if len(opts) == 0 {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}
	conn, err := grpc.NewClient(target, opts...)
	if err != nil {
		return nil, qerr.Transform("grpc", "dial", err)
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

func (c *GRPCClient) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}
