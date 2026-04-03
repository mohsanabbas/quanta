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

func (e *Engine) Run(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		if e.runner != nil {
			_ = e.runner.Close(ctx)
		}
		e.transport.Stop()
	}()

	return e.transport.Serve()
}
