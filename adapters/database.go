// Package database owns the Postgres connection and the schema
// migrations. It is infrastructure, not domain: the catalog core never
// imports it.
package database

import (
	"context"
	"embed"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

// Migrations are embedded in the binary: the production container does
// not carry a migrations directory, so the image and the schema cannot
// drift apart.
//
//go:embed migrations/*.sql
var migrationsFS embed.FS

// NewPool opens the connection pool.
//
// It points at Neon's DIRECT connection rather than the PgBouncer one.
// pgx keeps its own pool, and stacking it on transaction pooling breaks
// the prepared statement cache unless that cache is disabled — and with
// a single process and low concurrency there is nothing to gain in
// exchange.
func NewPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("invalid database URL: %w", err)
	}

	// The admin has very few concurrent users; a large pool would only
	// eat into the free tier's connection limit.
	cfg.MaxConns = 5

	// MinConns = 0 is deliberate. Neon's compute suspends when idle, and
	// holding a connection open would keep it awake, burning free-tier
	// compute hours while nobody is using the admin.
	cfg.MinConns = 0

	cfg.MaxConnIdleTime = 2 * time.Minute
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.HealthCheckPeriod = time.Minute
	cfg.ConnConfig.ConnectTimeout = 10 * time.Second

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("opening pool: %w", err)
	}

	// Fail early with a clear message instead of discovering the problem
	// on the first request. The timeout is generous because Neon's
	// compute may be asleep and take a moment to wake up.
	pingCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("database did not answer: %w", err)
	}

	return pool, nil
}

// Migrate applies pending migrations.
//
// It runs at startup. With a single machine that is safe and avoids a
// separate deploy step; if there is ever more than one instance this
// becomes a release_command in fly.toml so two machines do not migrate
// at the same time.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	goose.SetBaseFS(migrationsFS)
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("setting dialect: %w", err)
	}
	goose.SetLogger(goose.NopLogger())

	// goose speaks database/sql; stdlib adapts the pgx pool so there is
	// no second route to the database.
	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()

	if err := goose.UpContext(ctx, db, "migrations"); err != nil {
		return fmt.Errorf("applying migrations: %w", err)
	}
	return nil
}

// SchemaVersion returns the applied schema version, for logging.
func SchemaVersion(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()
	return goose.GetDBVersionContext(ctx, db)
}
