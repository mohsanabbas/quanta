package pipeline

import (
	"context"
	"errors"

	"quanta/internal/config"
	qerr "quanta/internal/errors"
	"quanta/internal/transform"
	"quanta/sink"
	"quanta/source"
)

func Compile(ctx context.Context, path string) (*Runner, error) {
	cfg, err := config.LoadPipelineSpec(path)
	if err != nil {
		return nil, qerr.Pipeline("config", err)
	}

	coord := NewAckCoordinator()
	r := NewRunner(coord)

	if err := compileSource(ctx, cfg, r); err != nil {
		return nil, err
	}
	if err := compileTransformers(ctx, cfg, r); err != nil {
		return nil, err
	}
	if err := compileSinks(ctx, cfg, r); err != nil {
		return nil, err
	}
	if err := compileDLQ(ctx, cfg, r); err != nil {
		return nil, err
	}

	return r, nil
}

func compileSource(ctx context.Context, cfg config.PipelineConfig, r *Runner) error {
	src, err := source.New(cfg.Source.Kind)
	if err != nil {
		return qerr.Source(cfg.Source.Kind, "create", err)
	}

	confPath := cfg.Source.ResolvedConfigPath()
	if confPath == "" {
		return qerr.Config(cfg.Source.Kind, "resolve", errors.New("missing config_file for source"))
	}

	sourceCfg, err := source.LoadConfig(cfg.Source.Kind, confPath)
	if err != nil {
		return qerr.Config(cfg.Source.Kind, "load", err)
	}

	if err := src.Configure(ctx, sourceCfg); err != nil {
		return qerr.Source(cfg.Source.Kind, "configure", err)
	}

	r.SetSource(src)
	r.SubscribeAck(src.OnAck)
	return nil
}

func compileTransformers(ctx context.Context, cfg config.PipelineConfig, r *Runner) error {
	for _, t := range cfg.Transformers {
		cli, err := newTransformClient(ctx, t)
		if err != nil {
			return err
		}
		var errSink sink.Adapter
		if t.ErrorSink != nil {
			drv, proto, sinkErr := sink.New(t.ErrorSink.Sink)
			if sinkErr != nil {
				return qerr.Sink(t.ErrorSink.Sink, "create-error-sink", sinkErr)
			}
			if t.ErrorSink.Config.Node != nil {
				if decErr := sink.DecodeYAML(t.ErrorSink.Config.Node, proto); decErr != nil {
					return qerr.Config(t.ErrorSink.Sink, "decode-error-sink", decErr)
				}
			}
			if cfgErr := drv.Configure(ctx, proto); cfgErr != nil {
				return qerr.Sink(t.ErrorSink.Sink, "configure-error-sink", cfgErr)
			}
			errSink = drv
		}
		r.AddTransformer(t.Name, cli, t.Timeout(), t.Retry.Attempts, t.RetryBackoff(), errSink)
	}
	return nil
}

func newTransformClient(ctx context.Context, t config.TransformerConfig) (transform.Client, error) {
	switch t.Type {
	case "grpc":
		cli, err := transform.NewGRPCClient(ctx, t.Address)
		if err != nil {
			return nil, qerr.Transform(t.Name, "dial", err)
		}
		return cli, nil
	case "inproc":
		return nil, qerr.Transform(t.Name, "create", errors.New("in-proc transformer not yet supported"))
	default:
		return nil, qerr.Transform(t.Name, "create", errors.New("unsupported transformer type"))
	}
}

func compileSinks(ctx context.Context, cfg config.PipelineConfig, r *Runner) error {
	for _, name := range cfg.Sinks {
		drv, proto, err := sink.New(name)
		if err != nil {
			return qerr.Sink(name, "create", err)
		}

		sinkCfg := proto
		if node := cfg.SinkConfig(name); node != nil {
			if err := sink.DecodeYAML(node, proto); err != nil {
				return qerr.Config(name, "decode", err)
			}
			sinkCfg = proto
		}

		if err := drv.Configure(ctx, sinkCfg); err != nil {
			return qerr.Sink(name, "configure", err)
		}

		r.AddSink(drv)
	}
	return nil
}

func compileDLQ(ctx context.Context, cfg config.PipelineConfig, r *Runner) error {
	if cfg.DLQ == nil || !cfg.DLQ.Enabled {
		return nil
	}

	drv, proto, err := sink.New(cfg.DLQ.Sink)
	if err != nil {
		return qerr.Sink(cfg.DLQ.Sink, "create-dlq", err)
	}

	sinkCfg := proto
	if cfg.DLQ.Config.Node != nil {
		if err := sink.DecodeYAML(cfg.DLQ.Config.Node, proto); err != nil {
			return qerr.Config(cfg.DLQ.Sink, "decode-dlq", err)
		}
		sinkCfg = proto
	}

	if err := drv.Configure(ctx, sinkCfg); err != nil {
		return qerr.Sink(cfg.DLQ.Sink, "configure-dlq", err)
	}

	r.SetDLQSink(drv)
	return nil
}
