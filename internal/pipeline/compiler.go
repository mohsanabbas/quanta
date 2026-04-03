package pipeline

import (
	"context"
	"fmt"

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

	r := NewRunner()

	if err := compileSource(ctx, cfg, r); err != nil {
		return nil, err
	}
	if err := compileTransformers(ctx, cfg, r); err != nil {
		return nil, err
	}
	if err := compileSinks(ctx, cfg, r); err != nil {
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
		return qerr.Config(cfg.Source.Kind, "resolve", fmt.Errorf("missing config_file for source %q", cfg.Source.Kind))
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
		r.AddTransformer(t.Name, cli, t.Timeout(), t.Retry.Attempts, t.RetryBackoff())
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
		return nil, qerr.Transform(t.Name, "create", fmt.Errorf("in-proc transformer not yet supported"))
	default:
		return nil, qerr.Transform(t.Name, "create", fmt.Errorf("unsupported transformer type %q", t.Type))
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

		if ackAware, ok := drv.(sink.AckAware); ok {
			ackAware.BindAck(r.Ack)
		}
		r.AddSink(drv)
	}
	return nil
}
