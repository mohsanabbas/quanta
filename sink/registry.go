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

// EmitFn is invoked by an ack-aware sink when a frame has been durably accepted
// downstream. The token identifies the source checkpoint to commit.
type EmitFn func(ctx context.Context, tok *pb.CheckpointToken)

// NackFn is invoked by a nack-aware sink when a frame has permanently failed
// delivery. The runner forwards the frame to the DLQ (if configured) and
// withholds the source commit otherwise.
type NackFn func(ctx context.Context, frame *pb.Frame, err error)

// Capabilities advertises which delivery callbacks a sink will fire.
//
// Drivers declare these once at construction. The runner uses them to decide
// barrier reference counts (each ack-aware sink contributes one ref; sync-only
// sinks contribute one shared ref).
type Capabilities struct {
	// AckAware is true if the sink fires the BuildOptions.Ack callback for
	// every successfully delivered frame.
	AckAware bool
	// NackAware is true if the sink fires the BuildOptions.Nack callback for
	// every permanently-failed frame.
	NackAware bool
}

// Adapter is the interface every sink driver implements.
type Adapter interface {
	// Name returns the adapter's identifier (e.g. "kafka", "s3", "stdout").
	// Used for error attribution and structured logging.
	Name() string
	// Caps reports which delivery callbacks this sink fires.
	Caps() Capabilities
	// Publish enqueues a frame for delivery. May block on backpressure.
	// Returning an error indicates a publish-time failure (the caller may
	// retry or DLQ); ack/nack callbacks signal post-enqueue outcomes for
	// ack-aware sinks.
	Publish(ctx context.Context, frame *pb.Frame) error
	// Close drains in-flight work and releases resources. Implementations
	// must be safe to call once; subsequent calls are no-ops.
	Close(ctx context.Context) error
}

// BuildOptions carries everything a driver needs at construction beyond its
// own configuration.
type BuildOptions struct {
	Ack  EmitFn
	Nack NackFn
}

// Registration declares a sink driver to the registry.
//
// DecodeConfig accepts the raw config (from YAML node, file path, or already-
// decoded struct) and returns a typed config the New factory understands.
// New constructs and fully initialises an Adapter using that config plus the
// runner-supplied BuildOptions. On failure, New must release any partially
// acquired resources.
type Registration struct {
	Name         string
	DecodeConfig func(raw any) (any, error)
	New          func(ctx context.Context, cfg any, opts BuildOptions) (Adapter, error)
}

var (
	_registryMu sync.RWMutex
	_registry   = map[string]Registration{}
)

// Register adds a driver to the registry. Panics on missing fields — sinks
// register from init() and a missing field is always a programmer error.
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

// Lookup returns the Registration for name, or false if not registered.
func Lookup(name string) (Registration, bool) {
	_registryMu.RLock()
	reg, ok := _registry[name]
	_registryMu.RUnlock()
	return reg, ok
}

// Build resolves the driver, decodes the raw config, and constructs the
// adapter — single-call replacement for the old New + DecodeConfig + Configure
// + BindAck + BindNack sequence.
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
