package telemetry

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"quanta/internal/logging"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	_readTimeout     = 5 * time.Second
	_writeTimeout    = 10 * time.Second
	_idleTimeout     = 120 * time.Second
	_shutdownTimeout = 5 * time.Second
)

func Expose(ctx context.Context, port int) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		Handler:           mux,
		ReadTimeout:       _readTimeout,
		ReadHeaderTimeout: _readTimeout,
		WriteTimeout:      _writeTimeout,
		IdleTimeout:       _idleTimeout,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), _shutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logging.Warnf("metrics server shutdown: %s", err)
		}
	}()

	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logging.Warnf("metrics server: %s", err)
		}
	}()
}
