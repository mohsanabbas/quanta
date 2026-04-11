package kafka

import (
	"context"
	"fmt"

	pb "quanta/api/proto/v1"
	qerr "quanta/internal/errors"
	"quanta/source"
)

func init() {
	source.Register(source.Registration{
		Name: "kafka",
		New:  func() source.Adapter { return &sourceAdapter{driver: &SaramaDriver{}} },
		LoadConfig: func(path string) (any, error) {
			cfg, err := LoadConfig(path)
			if err != nil {
				return nil, err
			}
			return &cfg, nil
		},
	})
}

type sourceAdapter struct {
	driver Adapter
}

var _ source.Adapter = (*sourceAdapter)(nil)

func (a *sourceAdapter) Configure(ctx context.Context, cfg any) error {
	kc, ok := cfg.(*Config)
	if !ok {
		return qerr.Config("kafka", "configure", fmt.Errorf("unexpected config type %T", cfg))
	}
	return a.driver.Configure(ctx, *kc)
}

func (a *sourceAdapter) Run(ctx context.Context, emit source.EmitFunc) error {
	return a.driver.Run(ctx, EmitFunc(emit))
}

func (a *sourceAdapter) OnAck(ack *pb.ConnectorAck) {
	a.driver.OnAck(ack)
}

func (a *sourceAdapter) Close(ctx context.Context) error {
	return a.driver.Close(ctx)
}
