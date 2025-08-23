package pipeline

import (
	"fmt"
	"os"
	pb "quanta/api/proto/v1"
	"time"

	"gopkg.in/yaml.v3"

	"quanta/internal/spec"
	"quanta/sink"
	"quanta/sink/stdout"
	"quanta/source/kafka"
)

func LoadYAML(path string, r *Runner) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var cfg spec.File
	if err = yaml.Unmarshal(raw, &cfg); err != nil {
		return err
	}

	/*──────── source (Kafka only for v0.1) ───────*/
	if cfg.Source.Kind != "kafka" {
		return fmt.Errorf("unsupported source %q", cfg.Source.Kind)
	}
	kc, err := kafka.LoadConfig(cfg.Source.Config)
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

	// driver may want ConnectorAck
	if aw, ok := src.(interface{ OnAck(*pb.ConnectorAck) }); ok {
		r.SubscribeAck(aw.OnAck)
	}

	/*──────── sinks ───────*/
	for _, name := range cfg.Sinks {
		sDrv, err := sink.NewAdapter(name)
		if err != nil {
			return err
		}

		switch name {
		case "stdout":
			delay := time.Duration(cfg.Debug.PerFrameDelayMS) * time.Millisecond
			err = sDrv.Configure(stdout.Config{
				DelayMS:      int(delay / time.Millisecond),
				PrintCounter: cfg.Debug.PrintCounter,
				BatchSize:    cfg.Debug.AckBatchSize,
				FlushMS:      cfg.Debug.AckFlushMS,
			})

		/* future:
		case "kafka":
			err = sDrv.Configure(cfg.SinkConfigs.Kafka)
		*/

		default:
			err = fmt.Errorf("no config block for sink %q", name)
		}
		if err != nil {
			return err
		}

		// If the sink supports acks, bind it.
		if ackAware, ok := sDrv.(sink.AckAware); ok {
			ackAware.BindAck(r.Ack)
		}
		r.AddSink(sDrv)
	}
	return nil
}
