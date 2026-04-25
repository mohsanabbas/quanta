package transform

import (
	"context"

	pb "quanta/api/proto/v1"
)

type Client interface {
	Metadata(ctx context.Context) (*pb.MetadataResponse, error)
	Health(ctx context.Context) (*pb.HealthResponse, error)
	Transform(ctx context.Context, req *pb.TransformRequest) (*pb.TransformResponse, error)
	Close() error
}
