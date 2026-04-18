package kafka

import (
	"context"

	pb "quanta/api/proto/v1"
	"quanta/source"
)

// EmitFunc aliases source.EmitFunc so internal Kafka code can refer to it
// without importing the parent package at every call site.
type EmitFunc = source.EmitFunc

// Compile-time assertion that the Frame type the parent package uses matches
// what this driver emits.
var _ EmitFunc = (func(context.Context, *pb.Frame) error)(nil)

// Adapter is the internal interface satisfied by Kafka source drivers.
// Drivers are constructed via package-private factories; there is no
// configure-after-construct step.
type Adapter interface {
	Run(ctx context.Context, emit EmitFunc) error
	OnAck(ack *pb.ConnectorAck)
	Close(ctx context.Context) error
}
