package transform

import (
	"context"

	pb "quanta/api/proto/v1"
)

// Client is the unary RPC surface the engine uses to drive a transformer.
// Streaming is intentionally absent — if/when bidi streaming lands it will
// be a separate, opt-in interface so that non-streaming implementations
// (in-process plugins) don't have to fake it.
type Client interface {
	Metadata(ctx context.Context) (*pb.MetadataResponse, error)
	Health(ctx context.Context) (*pb.HealthResponse, error)
	Transform(ctx context.Context, req *pb.TransformRequest) (*pb.TransformResponse, error)
	Close() error
}
