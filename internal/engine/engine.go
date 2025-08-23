package engine

import (
	"context"
	"quanta/internal/pipeline"
	"quanta/internal/transport"
)

type Engine struct {
	tp *transport.Server
	pl *pipeline.Runner
}

// Run blocks until ctx.Done().
func (e *Engine) Run(ctx context.Context) error {

	go func() {
		<-ctx.Done()
		e.tp.Stop()
	}()

	// Blocking: serve gRPC
	return e.tp.Serve()
}
