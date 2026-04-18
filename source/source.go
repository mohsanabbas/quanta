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

// EmitFunc is the callback the engine passes to a running source. The source
// invokes it once per delivered frame; a non-nil return signals that the
// runner has rejected the frame (engine-shutdown or pipeline error).
type EmitFunc func(context.Context, *pb.Frame) error

// Adapter is the interface every source driver implements.
type Adapter interface {
	// Run blocks until ctx is cancelled or the source terminates fatally.
	Run(ctx context.Context, emit EmitFunc) error
	// OnAck is invoked by the runner when a checkpoint barrier completes.
	// Sources translate the ack into source-specific commit semantics.
	OnAck(ack *pb.ConnectorAck)
	// Close drains in-flight work and releases resources.
	Close(ctx context.Context) error
}

// Registration declares a source driver to the registry.
//
// LoadConfig reads and decodes a config file; New constructs and fully
// initialises an Adapter using that config. On failure, New must release any
// partially acquired resources.
type Registration struct {
	Name       string
	LoadConfig func(path string) (any, error)
	New        func(ctx context.Context, cfg any) (Adapter, error)
}

var (
	_registryMu sync.RWMutex
	_registry   = map[string]Registration{}
)

// Register adds a driver to the registry. Panics on missing fields — sources
// register from init() and a missing field is always a programmer error.
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

// Lookup returns the Registration for name, or false if not registered.
func Lookup(name string) (Registration, bool) {
	_registryMu.RLock()
	reg, ok := _registry[name]
	_registryMu.RUnlock()
	return reg, ok
}

// LoadConfig resolves the driver and reads its config from path.
func LoadConfig(name, path string) (any, error) {
	reg, ok := Lookup(name)
	if !ok {
		return nil, qerr.Source(name, "load-config", errors.New("unknown adapter"))
	}
	return reg.LoadConfig(path)
}

// Build resolves the driver and constructs the adapter.
func Build(ctx context.Context, name string, cfg any) (Adapter, error) {
	reg, ok := Lookup(name)
	if !ok {
		return nil, qerr.Source(name, "build", errors.New("unknown adapter"))
	}
	return reg.New(ctx, cfg)
}
