// Package sink defines the sink Adapter interface and registration mechanism.
//
// Sinks are constructed via factories registered at init-time. The factory
// receives a decoded config and BuildOptions (ack/nack callbacks), and must
// return a fully-initialised, ready-to-Publish adapter — or clean up any
// partial state and return an error.
//
// Two-step Configure is gone: there is no valid "constructed but not configured"
// state for a sink anymore.
package sink

import (
	"context"
	"errors"
	"sync"

	pb "quanta/api/proto/v1"
	qerr "quanta/internal/errors"
)

type EmitFn func(ctx context.Context, tok *pb.CheckpointToken)

type NackFn func(ctx context.Context, frame *pb.Frame, err error)

type Capabilities struct {
	AckAware bool

	NackAware bool
}

type Adapter interface {
	Name() string

	Caps() Capabilities

	Publish(ctx context.Context, frame *pb.Frame) error

	Close(ctx context.Context) error
}

type BuildOptions struct {
	Ack  EmitFn
	Nack NackFn
}

type Registration struct {
	Name         string
	DecodeConfig func(raw any) (any, error)
	New          func(ctx context.Context, cfg any, opts BuildOptions) (Adapter, error)
}

var (
	_registryMu sync.RWMutex
	_registry   = map[string]Registration{}
)

func Register(r Registration) {
	if r.Name == "" {
		panic("sink: registration missing name")
	}
	if r.New == nil {
		panic("sink: registration " + r.Name + " missing New factory")
	}
	if r.DecodeConfig == nil {
		panic("sink: registration " + r.Name + " missing DecodeConfig")
	}
	_registryMu.Lock()
	_registry[r.Name] = r
	_registryMu.Unlock()
}

func Lookup(name string) (Registration, bool) {
	_registryMu.RLock()
	reg, ok := _registry[name]
	_registryMu.RUnlock()
	return reg, ok
}

func Build(ctx context.Context, name string, raw any, opts BuildOptions) (Adapter, error) {
	_registryMu.RLock()
	reg, ok := _registry[name]
	_registryMu.RUnlock()
	if !ok {
		return nil, qerr.Sink(name, "build", errors.New("unknown adapter"))
	}
	cfg, err := reg.DecodeConfig(raw)
	if err != nil {
		return nil, qerr.Config(name, "decode", err)
	}
	return reg.New(ctx, cfg, opts)
}
