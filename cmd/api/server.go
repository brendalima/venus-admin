package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

func newServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:    addr,
		Handler: handler,

		// Without these limits the server is exposed to connections that
		// open and never finish sending headers (Slowloris).
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       90 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MiB
	}
}

// serve runs srv until ctx is cancelled, then shuts it down gracefully.
func serve(ctx context.Context, logger *slog.Logger, srv *http.Server) error {
	errCh := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		logger.Info("signal received, shutting down")
	}

	// ctx is already cancelled, so the shutdown gets a fresh deadline.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	logger.Info("stopped")
	return nil
}
