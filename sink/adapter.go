package sink

import (
	"fmt"
	pb "quanta/api/proto/v1"
)

// EmitFn is what a sink calls to notify the pipeline that a frame
// (or a batch of frames) has been durably processed.
type EmitFn func(*pb.CheckpointToken)

// Adapter is the common behaviour every sink exposes.
type Adapter interface {
	Configure(any) error  // driver-specific YAML ⇒ struct
	Push(*pb.Frame) error // consume one frame
	Close() error         // idempotent
}

// AckAware is *optional*; sinks that need to emit ConnectorAck(s)
// simply implement it.  The compiler wires the callback if present.
type AckAware interface {
	BindAck(EmitFn)
}

/*──────── registry ───────*/

type factory = func() Adapter

var reg = map[string]factory{}

func Register(name string, f factory) { reg[name] = f }

func NewAdapter(name string) (Adapter, error) {
	if f, ok := reg[name]; ok {
		return f(), nil
	}
	return nil, fmt.Errorf("unknown sink %q", name)
}
