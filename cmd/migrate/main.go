// Command migrate applies the pending schema migrations and exits.
//
// It runs as Fly's release_command, before the new API version starts,
// so two API machines can never migrate at the same time.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/brendalima/venus-admin/adapters"
)

func main() {
	if err := run(); err != nil {
		slog.Error("migration failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := adapters.LoadConfig()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := adapters.NewDatabase(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	if err := adapters.Migrate(ctx, db); err != nil {
		return err
	}

	version, err := adapters.SchemaVersion(ctx, db)
	if err != nil {
		return err
	}
	slog.Info("schema up to date", "version", version)
	return nil
}
