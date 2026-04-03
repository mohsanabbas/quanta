package kafka

import (
	"context"

	pb "quanta/api/proto/v1"
)

type EmitFunc func(context.Context, *pb.Frame) error

type Adapter interface {
	Configure(ctx context.Context, cfg Config) error
	Run(ctx context.Context, emit EmitFunc) error
	OnAck(ack *pb.ConnectorAck)
	Close(ctx context.Context) error
}
