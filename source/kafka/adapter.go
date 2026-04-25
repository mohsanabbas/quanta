package kafka

import (
	"context"

	pb "quanta/api/proto/v1"
	"quanta/source"
)

type EmitFunc = source.EmitFunc

var _ EmitFunc = (func(context.Context, *pb.Frame) error)(nil)

type Adapter interface {
	Run(ctx context.Context, emit EmitFunc) error
	OnAck(ack *pb.ConnectorAck)
	Close(ctx context.Context) error
}
