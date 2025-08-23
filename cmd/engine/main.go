package main

import (
	"context"
	"os"
	"os/signal"
	"quanta/internal/logging"
	"quanta/source/kafka"
	"syscall"

	"quanta/internal/engine"
)

func main() {
	logging.InitFromEnv()
	pipelinePath := os.Getenv("QUANTA_PIPELINE_YML")
	if pipelinePath == "" {
		pipelinePath = "pipeline.yml"
	}
	cfg := engine.Config{
		GRPCPort:    7070,
		MetricsPort: 9100,
		PipelineYml: pipelinePath,
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	kafka.Register("sarama", func() kafka.Adapter { return &kafka.SaramaDriver{} })

	e, err := engine.Bootstrap(ctx, cfg)
	if err != nil {
		logging.L().Error("bootstrap failed", "err", err)
		os.Exit(1)
	}

	if err := e.Run(ctx); err != nil {
		logging.L().Error("engine run failed", "err", err)
		os.Exit(1)
	}
}
