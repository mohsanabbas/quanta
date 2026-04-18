package stdout

import (
	"context"
	"errors"

	"quanta/internal/config"
	qerr "quanta/internal/errors"
	"quanta/sink"
)

func init() {
	sink.Register(sink.Registration{
		Name:         "stdout",
		DecodeConfig: decodeConfig,
		New:          newStdoutSink,
	})
}

func decodeConfig(raw any) (any, error) {
	if raw == nil {
		return Config{}, nil
	}
	switch v := raw.(type) {
	case Config:
		return v, nil
	case *Config:
		if v == nil {
			return Config{}, nil
		}
		return *v, nil
	}
	var cfg Config
	if err := config.DecodeYAML(raw, &cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func newStdoutSink(_ context.Context, raw any, opts sink.BuildOptions) (sink.Adapter, error) {
	cfg, ok := raw.(Config)
	if !ok {
		return nil, qerr.Sink("stdout", "build", errors.New("unexpected config type"))
	}
	return newStdoutDriver(cfg, opts), nil
}
