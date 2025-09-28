package telemetry

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func Expose(port int) {
	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.Handler())

		srv := &http.Server{
			Addr:              fmt.Sprintf(":%d", port),
			Handler:           mux,
			ReadTimeout:       5 * time.Second,
			ReadHeaderTimeout: 5 * time.Second,
			WriteTimeout:      10 * time.Second,
			IdleTimeout:       120 * time.Second,
		}

		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("metrics server failed: %v", err)
		}
	}()
}
