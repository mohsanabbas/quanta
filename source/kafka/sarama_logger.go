package kafka

import (
	"context"
	"fmt"
	"log/slog"
)

func (d *SaramaDriver) logger(attrs ...slog.Attr) *slog.Logger {
	combined := append(append([]slog.Attr{}, d.baseAttrs...), attrs...)
	return logger(combined...)
}

func (d *SaramaDriver) loggerWithContext(ctx context.Context, attrs ...slog.Attr) *slog.Logger {
	combined := append(append([]slog.Attr{}, d.baseAttrs...), attrs...)
	return loggerFromContext(ctx, combined...)
}

type saramaSlogAdapter struct {
	logger *slog.Logger
}

func (s *saramaSlogAdapter) Print(v ...interface{}) {
	s.logger.Debug("sarama", slog.Any("args", v))
}

func (s *saramaSlogAdapter) Println(v ...interface{}) {
	s.logger.Debug("sarama", slog.Any("args", v))
}

func (s *saramaSlogAdapter) Printf(format string, v ...interface{}) {
	s.logger.Debug("sarama", slog.String("message", fmt.Sprintf(format, v...)))
}

type saramaNoopLogger struct{}

func (saramaNoopLogger) Print(...interface{})          {}
func (saramaNoopLogger) Println(...interface{})        {}
func (saramaNoopLogger) Printf(string, ...interface{}) {}
