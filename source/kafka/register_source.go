package kafka

import (
	"context"
	"fmt"

	qerr "quanta/internal/errors"
	"quanta/source"
)

func init() {
	source.Register(source.Registration{
		Name:       "kafka",
		LoadConfig: loadConfigFromPath,
		New:        newKafkaSource,
	})
}

func loadConfigFromPath(path string) (any, error) {
	return LoadConfig(path)
}

func newKafkaSource(ctx context.Context, raw any) (source.Adapter, error) {
	cfg, ok := raw.(Config)
	if !ok {
		return nil, qerr.Source("kafka", "build", fmt.Errorf("unexpected config type %T", raw))
	}
	return newSaramaDriver(ctx, cfg)
}
