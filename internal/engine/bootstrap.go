package engine

import (
	"context"

	qerr "quanta/internal/errors"
	"quanta/internal/pipeline"
	"quanta/internal/telemetry"
	"quanta/internal/transport"
)

func Bootstrap(ctx context.Context, cfg Config) (*Engine, error) {
	srv, err := transport.StartServer(cfg.GRPCPort)
	if err != nil {
		return nil, qerr.Transport("grpc", "start", err)
	}

	var runner *pipeline.Runner
	if cfg.PipelineYml != "" {
		runner, err = pipeline.Compile(ctx, cfg.PipelineYml)
		if err != nil {
			return nil, qerr.Pipeline("compile", err)
		}
		if err := runner.Start(ctx); err != nil {
			return nil, qerr.Pipeline("start", err)
		}
	}

	telemetry.Expose(ctx, cfg.MetricsPort)

	return &Engine{
		transport: srv,
		runner:    runner,
	}, nil
}
