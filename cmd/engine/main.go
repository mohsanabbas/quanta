package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"quanta/internal/engine"
	"quanta/internal/logging"
	"quanta/source/kafka"
)

func main() {
	logging.InitFromEnv()
	if err := run(); err != nil {
		logging.L().Error("engine failed", "err", err)
		os.Exit(1)
	}
}

func run() error {
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
	kafka.Register(kafka.Registration{
		Name: "sarama",
		New:  func() kafka.Adapter { return &kafka.SaramaDriver{} },
	})

	e, err := engine.Bootstrap(ctx, cfg)
	if err != nil {
		return fmt.Errorf("bootstrap failed: %w", err)
	}

	if err := e.Run(ctx); err != nil {
		return fmt.Errorf("engine run failed: %w", err)
	}
	return nil
}
