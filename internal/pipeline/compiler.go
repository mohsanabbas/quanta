package pipeline

import (
	"context"
	"fmt"
	"time"

	"gopkg.in/yaml.v3"
	pb "quanta/api/proto/v1"
	"quanta/internal/config"
	"quanta/internal/transform"
	"quanta/sink"
	sinkkafka "quanta/sink/kafka"
	"quanta/sink/stdout"
	"quanta/source/kafka"
)

const supportedPipelineSchema = "v1"

func Compile(path string) (*Runner, error) {
	r := NewRunner()
	if err := LoadYAML(path, r); err != nil {
		return nil, err
	}
	return r, nil
}

func LoadYAML(path string, r *Runner) error {
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
	if err = src.Configure(kc); err != nil {
		return err
	}
	r.SetSource(src)

	if aw, ok := src.(interface{ OnAck(*pb.ConnectorAck) }); ok {
		r.SubscribeAck(aw.OnAck)
	}

	for _, t := range cfg.Transformers {
		switch t.Type {
		case "grpc":
			cli, err := transform.NewGRPCClient(context.Background(), t.Address)
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
		sDrv, err := sink.NewAdapter(name)
		if err != nil {
			return err
		}

		switch name {
		case "stdout":
			delay := time.Duration(cfg.Debug.PerFrameDelayMS) * time.Millisecond
			err = sDrv.Configure(stdout.Config{
				DelayMS:       int(delay / time.Millisecond),
				PrintCounter:  cfg.Debug.PrintCounter,
				BatchSize:     cfg.Debug.AckBatchSize,
				FlushMS:       cfg.Debug.AckFlushMS,
				PrintValue:    cfg.Debug.PrintValue,
				ValueMaxBytes: cfg.Debug.ValueMaxBytes,
			})

		case "kafka":
			var sc sinkkafka.Config
			if err = decodeSinkConfig(cfg.SinkConfigs.Kafka, &sc); err != nil {
				err = fmt.Errorf("sink kafka: %w", err)
				break
			}
			err = sDrv.Configure(sc)
		default:
			err = fmt.Errorf("no config block for sink %q", name)
		}
		if err != nil {
			return err
		}

		if ackAware, ok := sDrv.(sink.AckAware); ok {
			ackAware.BindAck(r.Ack)
		}
		r.AddSink(sDrv)
	}
	return nil
}

func decodeSinkConfig[T any](in any, out *T) error {
	if in == nil {
		return fmt.Errorf("missing config")
	}
	raw, err := yaml.Marshal(in)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(raw, out)
}
