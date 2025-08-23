package sink

import (
	"fmt"
	pb "quanta/api/proto/v1"
)

type EmitFn func(*pb.CheckpointToken)

type Adapter interface {
	Configure(any) error
	Push(*pb.Frame) error
	Close() error
}

type AckAware interface {
	BindAck(EmitFn)
}

type factory = func() Adapter

var reg = map[string]factory{}

func Register(name string, f factory) { reg[name] = f }

func NewAdapter(name string) (Adapter, error) {
	if f, ok := reg[name]; ok {
		return f(), nil
	}
	return nil, fmt.Errorf("unknown sink %q", name)
}
