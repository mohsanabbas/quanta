package engine

import (
	"context"
	"fmt"
	"quanta/internal/pipeline"
	"quanta/internal/telemetry"
	"quanta/internal/transport"
)

func Bootstrap(ctx context.Context, cfg Config) (*Engine, error) {
	// 1. transport server
	srv, err := transport.StartServer(cfg.GRPCPort)
	if err != nil {
		return nil, fmt.Errorf("transport: %w", err)
	}

	// 2. pipeline runner
	var runner *pipeline.Runner
	if cfg.PipelineYml != "" {
		runner, err = pipeline.Compile(cfg.PipelineYml)
		if err != nil {
			return nil, fmt.Errorf("pipeline: %w", err)
		}
		if err := runner.Start(ctx); err != nil {
			return nil, err
		}
	}

	// 3. metrics
	telemetry.Expose(cfg.MetricsPort)

	return &Engine{
		transport: srv,
		runner:    runner,
	}, nil
}
