package pipeline

import (
	"context"
	"fmt"
	"time"

	pb "quanta/api/proto/v1"
	"quanta/internal/config"
	"quanta/internal/transform"
	"quanta/sink"

	"quanta/source/kafka"
)

func Compile(ctx context.Context, path string) (*Runner, error) {
	r := NewRunner()
	if err := LoadYAML(ctx, path, r); err != nil {
		return nil, err
	}
	return r, nil
}

func LoadYAML(ctx context.Context, path string, r *Runner) error {
	registerBuiltins()

	cfg, confPath, err := config.LoadPipelineSpec(path)
	if err != nil {
		return err
	}

	if cfg.Source.Kind != "kafka" {
		return fmt.Errorf("unsupported source %q", cfg.Source.Kind)
	}
	kc, err := config.LoadKafkaConfig(confPath)
	if err != nil {
		return err
	}

	src, err := kafka.NewAdapter(cfg.Source.Driver)
	if err != nil {
		return err
	}
	if err = src.Configure(ctx, kc); err != nil {
		return err
	}
	r.SetSource(src)

	if aw, ok := src.(interface{ OnAck(*pb.ConnectorAck) }); ok {
		r.SubscribeAck(aw.OnAck)
	}

	for _, t := range cfg.Transformers {
		switch t.Type {
		case "grpc":
			cli, err := transform.NewGRPCClient(ctx, t.Address)
			if err != nil {
				return fmt.Errorf("transform %s: dial %s: %w", t.Name, t.Address, err)
			}
			to := time.Duration(t.TimeoutMS) * time.Millisecond
			attempts := t.RetryPolicy.Attempts
			backoff := time.Duration(t.RetryPolicy.BackoffMS) * time.Millisecond
			r.AddTransformer(t.Name, cli, to, attempts, backoff)
		default:
			return fmt.Errorf("unsupported transformer type %q for %s", t.Type, t.Name)
		}
	}

	for _, name := range cfg.Sinks {
		sDrv, proto, err := sink.New(name)
		if err != nil {
			return err
		}

		var conf any
		switch name {
		case "stdout":
			if cfg.SinkConfigs.Stdout == nil {
				conf = proto
			} else {
				if err := sink.DecodeYAML(cfg.SinkConfigs.Stdout, proto); err != nil {
					return fmt.Errorf("sink stdout: %w", err)
				}
				conf = proto
			}
		case "kafka":
			if err := sink.DecodeYAML(cfg.SinkConfigs.Kafka, proto); err != nil {
				return fmt.Errorf("sink kafka: %w", err)
			}
			conf = proto
		default:
			return fmt.Errorf("no config block for sink %q", name)
		}

		if err := sDrv.Configure(ctx, conf); err != nil {
			return err
		}

		if ackAware, ok := sDrv.(sink.AckAware); ok {
			ackAware.BindAck(r.Ack)
		}
		r.AddSink(sDrv)
	}
	return nil
}
