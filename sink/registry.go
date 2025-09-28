package sink

import (
	"context"
	"fmt"
	pb "quanta/api/proto/v1"
)

type EmitFn func(*pb.CheckpointToken)

type Adapter interface {
	Configure(ctx context.Context, cfg any) error
	Publish(ctx context.Context, frame *pb.Frame) error
	Close(ctx context.Context) error
}

type AckAware interface {
	BindAck(EmitFn)
}

type Registration struct {
	Name        string
	New         func() Adapter
	ConfigProto func() any
}

var registry = map[string]Registration{}

func Register(r Registration) {
	if r.Name == "" {
		panic("sink: registration missing name")
	}
	if r.New == nil {
		panic(fmt.Sprintf("sink: registration %q missing constructor", r.Name))
	}
	registry[r.Name] = r
}

func Lookup(name string) (Registration, bool) { reg, ok := registry[name]; return reg, ok }

func New(name string) (Adapter, any, error) {
	reg, ok := registry[name]
	if !ok {
		return nil, nil, fmt.Errorf("sink: unknown adapter %q", name)
	}
	inst := reg.New()
	var cfg any
	if reg.ConfigProto != nil {
		cfg = reg.ConfigProto()
	}
	return inst, cfg, nil
}
