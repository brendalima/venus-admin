package adapters

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

// Migrations are embedded in the binary so the image and the schema
// cannot drift apart. They are applied by cmd/migrate, never by the API.
//
//go:embed migrations/*.sql
var migrationsFS embed.FS

// NewDatabase opens a database/sql handle over the pgx driver.
//
// It expects Neon's DIRECT connection rather than the PgBouncer one; see
// databaseURL in config.go.
func NewDatabase(ctx context.Context, dsn string) (*sql.DB, error) {
	connCfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("invalid database URL: %w", err)
	}
	connCfg.ConnectTimeout = 10 * time.Second

	db := stdlib.OpenDB(*connCfg)

	// The admin has very few concurrent users; a large pool would only
	// eat into the free tier's connection limit.
	db.SetMaxOpenConns(5)

	// Idle connections are closed quickly on purpose. Neon's compute
	// suspends when idle, and holding a connection open would keep it
	// awake, burning free-tier compute hours while nobody uses the admin.
	db.SetConnMaxIdleTime(2 * time.Minute)
	db.SetConnMaxLifetime(30 * time.Minute)

	// Fail early with a clear message instead of discovering the problem
	// on the first request. The timeout is generous because Neon's
	// compute may be asleep and take a moment to wake up.
	pingCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	if err := db.PingContext(pingCtx); err != nil {
		db.Close()
		return nil, fmt.Errorf("database did not answer: %w", err)
	}

	return db, nil
}

// Migrate applies pending migrations.
//
// It holds a Postgres advisory lock while it runs, so two migrators
// started at once take turns instead of both applying the same file.
// Fly's release_command already runs one at a time; the integration tests
// of different packages, run in parallel by go test, do not.
func Migrate(ctx context.Context, db *sql.DB) error {
	provider, err := newMigrationProvider(db)
	if err != nil {
		return err
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("couldn't apply migrations: %w", err)
	}
	return nil
}

// SchemaVersion returns the applied schema version, for logging.
func SchemaVersion(ctx context.Context, db *sql.DB) (int64, error) {
	provider, err := newMigrationProvider(db)
	if err != nil {
		return 0, err
	}
	return provider.GetDBVersion(ctx)
}

func newMigrationProvider(db *sql.DB) (*goose.Provider, error) {
	migrations, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("couldn't open embedded migrations: %w", err)
	}
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return nil, fmt.Errorf("couldn't create migration lock: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations, goose.WithSessionLocker(locker))
	if err != nil {
		return nil, fmt.Errorf("couldn't create migration provider: %w", err)
	}
	return provider, nil
}
