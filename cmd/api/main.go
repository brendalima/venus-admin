// Command api serves the Vênus Oasis admin.
//
// This is the composition root: the only place that knows every layer
// and wires them together. It holds no business rules.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/brendalima/venus-admin/adapters"
	"github.com/brendalima/venus-admin/internal/core/services"
	"github.com/brendalima/venus-admin/internal/repository"
	"github.com/brendalima/venus-admin/internal/rest"
)

func main() {
	if err := run(); err != nil {
		slog.Error("exiting on error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := adapters.LoadConfig()
	if err != nil {
		return err
	}

	logger := newLogger(cfg)
	slog.SetDefault(logger)
	logger.Info("starting", "env", cfg.Env, "port", cfg.Port, "go", runtime.Version())

	// Fly sends SIGTERM when stopping an idle machine, and in-flight
	// requests need to finish before the process exits.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := adapters.NewDatabase(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	hRepository := repository.MustNewHealth(db)
	hService := services.MustNewHealth(hRepository)
	rHealth := rest.MustNewHealth(hService)
	rAdmin := &rest.Admin{}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", rHealth.Live)
	mux.HandleFunc("GET /readyz", rHealth.Ready)
	mux.HandleFunc("GET /{$}", rAdmin.Home)
	mux.HandleFunc("GET /admin", rAdmin.Index)

	handler := rest.Chain(mux,
		rest.RecoverPanics(logger),
		rest.LogRequests(logger),
		rest.SecureHeaders,
	)

	return serve(ctx, logger, newServer(cfg.Addr(), handler))
}

func newLogger(cfg adapters.Config) *slog.Logger {
	if cfg.IsProd() {
		// JSON in production so Fly's log search is usable.
		return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	}
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
}
