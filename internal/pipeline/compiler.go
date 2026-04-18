package pipeline

import (
	"context"
	"errors"

	"gopkg.in/yaml.v3"

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
	confPath := cfg.Source.ResolvedConfigPath()
	if confPath == "" {
		return qerr.Config(cfg.Source.Kind, "resolve", errors.New("missing config_file for source"))
	}

	sourceCfg, err := source.LoadConfig(cfg.Source.Kind, confPath)
	if err != nil {
		return qerr.Config(cfg.Source.Kind, "load", err)
	}

	src, err := source.Build(ctx, cfg.Source.Kind, sourceCfg)
	if err != nil {
		return err
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
			drv, sinkErr := buildSink(ctx, r, t.ErrorSink.Sink, t.ErrorSink.Config.Node)
			if sinkErr != nil {
				return sinkErr
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
		drv, err := buildSink(ctx, r, name, cfg.SinkConfig(name))
		if err != nil {
			return err
		}
		r.AddSink(drv)
	}
	return nil
}

func compileDLQ(ctx context.Context, cfg config.PipelineConfig, r *Runner) error {
	if cfg.DLQ == nil || !cfg.DLQ.Enabled {
		return nil
	}
	drv, err := buildSink(ctx, r, cfg.DLQ.Sink, cfg.DLQ.Config.Node)
	if err != nil {
		return err
	}
	r.SetDLQSink(drv)
	return nil
}

// buildSink resolves a sink driver, decodes its raw config, and constructs
// the adapter with ack/nack callbacks bound to the runner's coordinator.
func buildSink(ctx context.Context, r *Runner, name string, raw any) (sink.Adapter, error) {
	opts := sink.BuildOptions{
		Ack:  r.coord.Ack,
		Nack: r.coord.Nack,
	}
	return sink.Build(ctx, name, normalizeRawConfig(raw), opts)
}

// normalizeRawConfig collapses a typed-nil *yaml.Node into an untyped nil so
// downstream DecodeConfig implementations can uniformly treat "no config" the
// same regardless of whether the YAML key was absent or explicitly null.
func normalizeRawConfig(raw any) any {
	if node, ok := raw.(*yaml.Node); ok && node == nil {
		return nil
	}
	return raw
}
