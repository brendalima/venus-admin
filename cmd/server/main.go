// Command server runs the Vênus Oasis admin.
//
// This file is the composition root of the hexagon: the only place that
// knows about both the core and every adapter, wiring one into the other.
// Nothing else imports concrete adapters.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/brendalima/venus-admin/internal/adapter/web"
	"github.com/brendalima/venus-admin/internal/platform/config"
	"github.com/brendalima/venus-admin/internal/platform/database"
	"github.com/joho/godotenv"
)

func main() {
	if err := run(); err != nil {
		slog.Error("exiting on error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	// In development, load .env.local (written by `neon init`) and then
	// .env. godotenv does not override what is already in the environment,
	// so an exported variable still wins.
	if os.Getenv("APP_ENV") != string(config.EnvProd) {
		_ = godotenv.Load(".env.local", ".env")
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger := newLogger(cfg)
	logger.Info("starting", "env", cfg.Env, "port", cfg.Port, "go", runtime.Version())

	var db web.Pinger
	if cfg.DatabaseURL == "" {
		logger.Warn("DATABASE_URL is not set — starting without a database")
	} else {
		connectCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		pool, err := database.NewPool(connectCtx, cfg.DatabaseURL)
		cancel()
		if err != nil {
			return err
		}
		defer pool.Close()

		if err := database.Migrate(context.Background(), pool); err != nil {
			return err
		}
		version, err := database.SchemaVersion(context.Background(), pool)
		if err != nil {
			return err
		}
		logger.Info("database connected", "schema_version", version)

		db = pool
	}

	srv := &http.Server{
		Addr:    cfg.Addr(),
		Handler: web.NewServer(cfg, logger, db).Handler(),

		// Without these limits the server is exposed to connections that
		// open and never finish sending headers (Slowloris).
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       90 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MiB
	}

	// Graceful shutdown: Fly sends SIGTERM when stopping an idle machine,
	// and in-flight requests need to finish before the process exits.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	logger.Info("stopped")
	return nil
}

func newLogger(cfg config.Config) *slog.Logger {
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}
	if cfg.IsProd() {
		// JSON in production so Fly's log search is usable.
		return slog.New(slog.NewJSONHandler(os.Stdout, opts))
	}
	opts.Level = slog.LevelDebug
	return slog.New(slog.NewTextHandler(os.Stdout, opts))
}
