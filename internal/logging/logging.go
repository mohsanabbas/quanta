package logging

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const (
	_fatalLevel = slog.LevelError + 4
	_callerSkip = 3
)

type Options struct {
	Level string
	JSON  bool
}

var _logger atomic.Pointer[slog.Logger]

func init() {
	cfg := &slog.HandlerOptions{Level: slog.LevelInfo}
	h := slog.NewTextHandler(os.Stderr, cfg)
	_logger.Store(slog.New(h))
}

func Configure(opts Options) {
	lvl := parseLevel(opts.Level)
	cfg := &slog.HandlerOptions{Level: lvl}
	var h slog.Handler
	if opts.JSON {
		h = slog.NewJSONHandler(os.Stderr, cfg)
	} else {
		h = slog.NewTextHandler(os.Stderr, cfg)
	}
	_logger.Store(slog.New(h))
}

func InitFromEnv() {
	lvl := os.Getenv("QUANTA_LOG_LEVEL")
	jsonStr := os.Getenv("QUANTA_LOG_JSON")
	json := false
	if b, err := strconv.ParseBool(strings.TrimSpace(jsonStr)); err == nil {
		json = b
	}
	Configure(Options{Level: lvl, JSON: json})
}

func L() *slog.Logger {
	return _logger.Load()
}

func Infof(format string, args ...any) {
	logf(slog.LevelInfo, format, args...)
}

func Warnf(format string, args ...any) {
	logf(slog.LevelWarn, format, args...)
}

func Errorf(format string, args ...any) {
	logf(slog.LevelError, format, args...)
}

func Fatalf(format string, args ...any) {
	logf(_fatalLevel, format, args...)
	os.Exit(1)
}

func logf(level slog.Level, format string, args ...any) {
	l := _logger.Load()
	ctx := context.Background()
	if !l.Enabled(ctx, level) {
		return
	}
	msg := fmt.Sprintf(format, args...)
	var pcs [1]uintptr
	runtime.Callers(_callerSkip, pcs[:])
	r := slog.NewRecord(time.Now(), level, msg, pcs[0])
	_ = l.Handler().Handle(ctx, r)
}

func parseLevel(s string) slog.Level {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
