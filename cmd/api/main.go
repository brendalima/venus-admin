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
	"time"

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
	uRepository := repository.MustNewUser(db)
	sRepository := repository.MustNewSession(db)

	hService := services.MustNewHealth(hRepository)
	uService := services.MustNewUser(uRepository, sRepository)

	templates := rest.MustNewTemplates()
	sessions := rest.MustNewSessionManager(sRepository, cfg.IsProd())
	rHealth := rest.MustNewHealth(hService)
	rAuth := rest.MustNewAuth(rest.AuthInput{
		Service:  uService,
		Sessions: sessions,
		// Ten tries in fifteen minutes is plenty for someone who mistypes,
		// and makes guessing a password hopeless.
		Limiter:        rest.NewLoginLimiter(10, 15*time.Minute),
		Templates:      templates,
		ClientIPHeader: cfg.ClientIPHeader,
	})
	rAdmin := rest.MustNewAdmin(templates)

	// Everything a person sees goes through the session; the health
	// checks stay outside it, so they never touch the database.
	app := http.NewServeMux()
	app.HandleFunc("GET /{$}", rAdmin.Home)
	app.HandleFunc("GET /login", rAuth.LoginPage)
	app.HandleFunc("POST /login", rAuth.Login)
	app.HandleFunc("POST /logout", rAuth.Logout)
	app.Handle("GET /admin", rAuth.RequireLogin(http.HandlerFunc(rAdmin.Index)))

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", rHealth.Live)
	mux.HandleFunc("GET /readyz", rHealth.Ready)
	mux.Handle("/", sessions.LoadAndSave(app))

	handler := rest.Chain(mux,
		rest.RecoverPanics(logger),
		rest.LogRequests(logger),
		rest.SecureHeaders,
		rest.RejectCrossOrigin,
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
