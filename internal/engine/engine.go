package engine

import (
	"context"
	"quanta/internal/pipeline"
	"quanta/internal/transport"
)

type Engine struct {
	transport *transport.Server
	runner    *pipeline.Runner
}

// Run blocks until ctx.Done().
func (e *Engine) Run(ctx context.Context) error {

	go func() {
		<-ctx.Done()
		e.transport.Stop()
		if e.runner != nil {
			_ = e.runner.Close()
		}
	}()

	// Blocking: serve gRPC
	return e.transport.Serve()
}
