package kafka

import (
	"context"
	pb "quanta/api/proto/v1"
)

type EmitFunc func(*pb.Frame) error

type Adapter interface {
	Configure(Config) error
	Run(context.Context, EmitFunc) error
	Close() error
}
