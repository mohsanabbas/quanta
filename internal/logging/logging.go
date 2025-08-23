package logging

import (
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
)

type Options struct {
	Level string // debug|info|warn|error
	JSON  bool
}

var def atomic.Value // holds *slog.Logger

func init() {
	cfg := &slog.HandlerOptions{Level: slog.LevelInfo}
	h := slog.NewTextHandler(os.Stderr, cfg)
	def.Store(slog.New(h))
}

// Configure replaces the default logger.
func Configure(opts Options) {
	lvl := parseLevel(opts.Level)
	cfg := &slog.HandlerOptions{Level: lvl}
	var h slog.Handler
	if opts.JSON {
		h = slog.NewJSONHandler(os.Stderr, cfg)
	} else {
		h = slog.NewTextHandler(os.Stderr, cfg)
	}
	def.Store(slog.New(h))
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

// L returns the configured default logger.
func L() *slog.Logger {
	l, _ := def.Load().(*slog.Logger)
	return l
}

// InitFromEnv configures the logger from environment variables.
// QUANTA_LOG_LEVEL: debug|info|warn|error (default info)
// QUANTA_LOG_JSON:  true|false (default false)
func InitFromEnv() {
	lvl := os.Getenv("QUANTA_LOG_LEVEL")
	jsonStr := os.Getenv("QUANTA_LOG_JSON")
	json := false
	if b, err := strconv.ParseBool(strings.TrimSpace(jsonStr)); err == nil {
		json = b
	}
	Configure(Options{Level: lvl, JSON: json})
}
