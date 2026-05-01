package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"quanta/internal/engine"
	"quanta/internal/logging"

	// Driver registration — blank imports trigger init().
	// Add or remove drivers here to control what the binary supports.
	_ "quanta/sink/clickhouse"
	_ "quanta/sink/kafka"
	_ "quanta/sink/s3"
	_ "quanta/sink/stdout"
	_ "quanta/source/kafka"
)

func main() {
	logging.InitFromEnv()
	if err := run(); err != nil {
		logging.L().Error("engine failed", "err", err)
		os.Exit(1)
	}
}

const (
	_defaultGRPCPort    = 7070
	_defaultMetricsPort = 9100
)

func run() error {
	pipelinePath := os.Getenv("QUANTA_PIPELINE_YML")
	if pipelinePath == "" {
		pipelinePath = "topology/pipeline.yml"
	}

	cfg := engine.Config{
		GRPCPort:    _defaultGRPCPort,
		MetricsPort: _defaultMetricsPort,
		PipelineYml: pipelinePath,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	e, err := engine.Bootstrap(ctx, cfg)
	if err != nil {
		return err
	}
	return e.Run(ctx)
}
