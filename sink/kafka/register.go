package kafka

import (
	"context"
	"errors"

	"quanta/internal/config"
	qerr "quanta/internal/errors"
	"quanta/sink"
)

func init() {
	sink.Register(sink.Registration{
		Name:         "kafka",
		DecodeConfig: decodeConfig,
		New:          newKafkaSink,
	})
}

func decodeConfig(raw any) (any, error) {
	switch v := raw.(type) {
	case Config:
		return v, nil
	case *Config:
		if v == nil {
			return nil, qerr.Config("kafka", "decode", errors.New("nil config"))
		}
		return *v, nil
	}
	var cfg Config
	if err := config.DecodeYAML(raw, &cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func newKafkaSink(ctx context.Context, raw any, opts sink.BuildOptions) (sink.Adapter, error) {
	cfg, ok := raw.(Config)
	if !ok {
		return nil, qerr.Sink("kafka", "build", errors.New("unexpected config type"))
	}
	return newSaramaSink(ctx, cfg, opts)
}
