package transform

import (
	"context"

	pb "quanta/api/proto/v1"

	"google.golang.org/grpc"
)

type Client interface {
	Metadata(ctx context.Context) (*pb.MetadataResponse, error)
	Health(ctx context.Context) (*pb.HealthResponse, error)
	Transform(ctx context.Context, req *pb.TransformRequest) (*pb.TransformResponse, error)
	Stream(ctx context.Context, opts ...grpc.CallOption) (pb.TransformService_TransformStreamClient, error)
	Close() error
}
