package kafka

import (
	"context"
	"log/slog"

	"quanta/internal/logging"

	"go.opentelemetry.io/otel/trace"
)

type logContextKey struct{}

// WithLogAttrs attaches slog attributes to the context so downstream logs can include them.
func WithLogAttrs(ctx context.Context, attrs ...slog.Attr) context.Context {
	if len(attrs) == 0 {
		return ctx
	}
	existing, _ := ctx.Value(logContextKey{}).([]slog.Attr)
	combined := append(make([]slog.Attr, 0, len(existing)+len(attrs)), existing...)
	combined = append(combined, attrs...)
	return context.WithValue(ctx, logContextKey{}, combined)
}

func loggerFromContext(ctx context.Context, attrs ...slog.Attr) *slog.Logger {
	base := withAttrs(logging.L(), slog.String("component", "source.kafka"))
	if ctx != nil {
		if ctxAttrs, ok := ctx.Value(logContextKey{}).([]slog.Attr); ok && len(ctxAttrs) > 0 {
			base = withAttrs(base, ctxAttrs...)
		}
		if span := trace.SpanFromContext(ctx); span != nil {
			if sc := span.SpanContext(); sc.IsValid() {
				base = withAttrs(base,
					slog.String("trace_id", sc.TraceID().String()),
					slog.String("span_id", sc.SpanID().String()),
				)
			}
		}
	}
	return withAttrs(base, attrs...)
}

func logger(attrs ...slog.Attr) *slog.Logger {
	return loggerFromContext(context.Background(), attrs...)
}

func withAttrs(logger *slog.Logger, attrs ...slog.Attr) *slog.Logger {
	if len(attrs) == 0 {
		return logger
	}
	args := make([]any, len(attrs))
	for i, attr := range attrs {
		args[i] = attr
	}
	return logger.With(args...)
}
