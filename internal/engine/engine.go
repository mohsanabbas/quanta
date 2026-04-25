package engine

import (
	"context"
	"log/slog"
	"time"

	"quanta/internal/pipeline"
	"quanta/internal/transport"

	"golang.org/x/sync/errgroup"
)

type Engine struct {
	transport *transport.Server
	runner    *pipeline.Runner
}

const _shutdownGrace = 10 * time.Second

func (e *Engine) Run(ctx context.Context) error {
	g, gCtx := errgroup.WithContext(ctx)

	g.Go(func() error {
		return e.watchLifecycle(gCtx)
	})

	g.Go(func() error {
		return e.transport.Serve()
	})

	err := g.Wait()

	e.shutdown(ctx)
	return err
}

func (e *Engine) watchLifecycle(ctx context.Context) error {
	if e.runner == nil {
		<-ctx.Done()
		e.transport.Stop()
		return ctx.Err()
	}

	select {
	case <-ctx.Done():
		e.transport.Stop()
		return ctx.Err()
	case err := <-e.runner.SourceErr():
		slog.Error("engine: source terminated unexpectedly", "error", err)
		e.transport.Stop()
		return err
	}
}

func (e *Engine) shutdown(ctx context.Context) {
	if e.runner == nil {
		return
	}
	slog.Warn("engine: draining pipeline")
	closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), _shutdownGrace)
	defer cancel()
	if err := e.runner.Close(closeCtx); err != nil {
		slog.Error("engine: runner close error", "error", err)
	}
}
