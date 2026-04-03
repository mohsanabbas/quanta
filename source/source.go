package source

import (
	"context"
	"errors"

	qerr "quanta/internal/errors"

	pb "quanta/api/proto/v1"
)

type EmitFunc func(context.Context, *pb.Frame) error

type Adapter interface {
	Configure(ctx context.Context, cfg any) error
	Run(ctx context.Context, emit EmitFunc) error
	OnAck(ack *pb.ConnectorAck)
	Close(ctx context.Context) error
}

type Registration struct {
	Name       string
	New        func() Adapter
	LoadConfig func(path string) (any, error)
}

var _registry = map[string]Registration{}

func Register(r Registration) {
	if r.Name == "" {
		panic("source: registration missing name")
	}
	if r.New == nil {
		panic("source: registration " + r.Name + " missing constructor")
	}
	_registry[r.Name] = r
}

func Lookup(name string) (Registration, bool) {
	reg, ok := _registry[name]
	return reg, ok
}

func New(name string) (Adapter, error) {
	reg, ok := _registry[name]
	if !ok {
		return nil, qerr.Source(name, "create", errors.New("unknown adapter"))
	}
	return reg.New(), nil
}

func LoadConfig(name, path string) (any, error) {
	reg, ok := _registry[name]
	if !ok {
		return nil, qerr.Source(name, "load-config", errors.New("unknown adapter"))
	}
	if reg.LoadConfig == nil {
		return nil, qerr.Config(name, "load-config", errors.New("no config loader registered"))
	}
	return reg.LoadConfig(path)
}
