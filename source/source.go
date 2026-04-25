// Package source defines the source Adapter interface and registration mechanism.
//
// Sources are constructed via factories registered at init-time. The factory
// receives a decoded config and returns a fully-initialised, ready-to-Run
// adapter. Two-step Configure is gone: there is no valid "constructed but not
// configured" state for a source anymore.
package source

import (
	"context"
	"errors"
	"sync"

	pb "quanta/api/proto/v1"
	qerr "quanta/internal/errors"
)

type EmitFunc func(context.Context, *pb.Frame) error

type Adapter interface {
	Run(ctx context.Context, emit EmitFunc) error

	OnAck(ack *pb.ConnectorAck)

	Close(ctx context.Context) error
}

type Registration struct {
	Name       string
	LoadConfig func(path string) (any, error)
	New        func(ctx context.Context, cfg any) (Adapter, error)
}

var (
	_registryMu sync.RWMutex
	_registry   = map[string]Registration{}
)

func Register(r Registration) {
	if r.Name == "" {
		panic("source: registration missing name")
	}
	if r.New == nil {
		panic("source: registration " + r.Name + " missing New factory")
	}
	if r.LoadConfig == nil {
		panic("source: registration " + r.Name + " missing LoadConfig")
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

func LoadConfig(name, path string) (any, error) {
	reg, ok := Lookup(name)
	if !ok {
		return nil, qerr.Source(name, "load-config", errors.New("unknown adapter"))
	}
	return reg.LoadConfig(path)
}

func Build(ctx context.Context, name string, cfg any) (Adapter, error) {
	reg, ok := Lookup(name)
	if !ok {
		return nil, qerr.Source(name, "build", errors.New("unknown adapter"))
	}
	return reg.New(ctx, cfg)
}
