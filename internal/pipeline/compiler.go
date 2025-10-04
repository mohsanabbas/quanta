package pipeline

import (
	"context"
	"fmt"

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

	cfg, err := config.LoadPipelineSpec(path)
	if err != nil {
		return err
	}

	if cfg.Source.Kind != "kafka" {
		return fmt.Errorf("unsupported source %q", cfg.Source.Kind)
	}
	sourceConfPath := cfg.Source.ResolvedConfigPath()
	if sourceConfPath == "" {
		return fmt.Errorf("unsupported inline source config for driver %q", cfg.Source.Driver)
	}
	kc, err := config.LoadKafkaConfig(sourceConfPath)
	if err != nil {
		return err
	}

	// Register default Kafka drivers to ensure the requested driver is available.
	kafka.RegisterDefaults()

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
			to := t.Timeout()
			attempts := t.Retry.Attempts
			backoff := t.RetryBackoff()
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
			node := cfg.SinkConfig(name)
			if node == nil {
				conf = proto
			} else {
				if err := sink.DecodeYAML(node, proto); err != nil {
					return fmt.Errorf("sink stdout: %w", err)
				}
				conf = proto
			}
		case "kafka":
			node := cfg.SinkConfig(name)
			if node == nil {
				return fmt.Errorf("sink kafka: missing configuration block")
			}
			if err := sink.DecodeYAML(node, proto); err != nil {
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
