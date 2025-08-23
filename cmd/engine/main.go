package main

import (
	"context"
	"log"
	"os/signal"
	"quanta/source/kafka"
	"syscall"

	"quanta/internal/engine"
)

func main() {
	cfg := engine.Config{
		GRPCPort:    7070,
		MetricsPort: 9100,
		PipelineYml: "pipeline.yml", // optional
		//PipelineYml: "", // optional
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	kafka.Register("sarama", func() kafka.Adapter { return &kafka.SaramaDriver{} })

	e, err := engine.Bootstrap(ctx, cfg)
	if err != nil {
		log.Fatalf("bootstrap: %v", err)
	}

	if err := e.Run(ctx); err != nil {
		log.Fatalf("engine: %v", err)
	}
}
