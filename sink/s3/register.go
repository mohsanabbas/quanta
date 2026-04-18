package s3

import (
	"context"
	"errors"

	"quanta/internal/config"
	qerr "quanta/internal/errors"
	"quanta/sink"
)

func init() {
	sink.Register(sink.Registration{
		Name:         "s3",
		DecodeConfig: decodeConfig,
		New:          newS3Sink,
	})
}

func decodeConfig(raw any) (any, error) {
	switch v := raw.(type) {
	case Config:
		return v, nil
	case *Config:
		if v == nil {
			return nil, qerr.Config("s3", "decode", errors.New("nil config"))
		}
		return *v, nil
	}
	var cfg Config
	if err := config.DecodeYAML(raw, &cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func newS3Sink(ctx context.Context, raw any, opts sink.BuildOptions) (sink.Adapter, error) {
	cfg, ok := raw.(Config)
	if !ok {
		return nil, qerr.Sink("s3", "build", qerr.Wrapf(errBadConfigType, "got %T", raw))
	}
	return newDriver(ctx, cfg, opts)
}
