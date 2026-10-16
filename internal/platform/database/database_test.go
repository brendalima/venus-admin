package database

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)

// Integration tests: they talk to a real Postgres.
// They skip when no database is configured, so CI stays green without
// holding the secret.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	_ = godotenv.Load("../../../.env.local", "../../../.env")

	dsn := os.Getenv("DATABASE_URL_UNPOOLED")
	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}
	if dsn == "" {
		t.Skip("no DATABASE_URL — skipping integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("connecting: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := Migrate(context.Background(), pool); err != nil {
		t.Fatalf("migrating: %v", err)
	}
	return pool
}

func TestSchemaIsApplied(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	for _, table := range []string{"products", "product_images", "site_state"} {
		var exists bool
		err := pool.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM information_schema.tables
			 WHERE table_schema = 'public' AND table_name = $1)`, table,
		).Scan(&exists)
		if err != nil {
			t.Fatalf("querying %s: %v", table, err)
		}
		if !exists {
			t.Errorf("table %s is missing", table)
		}
	}
}

func TestProductRoundTripAndUpdatedAtTrigger(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	const slug = "integration-test-do-not-use"
	cleanup := func() {
		_, _ = pool.Exec(ctx, `DELETE FROM products WHERE slug = $1`, slug)
	}
	cleanup()
	t.Cleanup(cleanup)

	var id int64
	var updatedAt time.Time
	err := pool.QueryRow(ctx,
		`INSERT INTO products (slug, name, price_cents, sizes, description, details, position)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING id, updated_at`,
		slug, "Test Bikini", 18900,
		[]string{"P", "M"}, "description", []string{"one detail"}, 99,
	).Scan(&id, &updatedAt)
	if err != nil {
		t.Fatalf("inserting: %v", err)
	}

	// Read back, confirming the arrays survive the round trip.
	var name string
	var cents int64
	var sizes, details []string
	err = pool.QueryRow(ctx,
		`SELECT name, price_cents, sizes, details FROM products WHERE id = $1`, id,
	).Scan(&name, &cents, &sizes, &details)
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if name != "Test Bikini" {
		t.Errorf("name = %q", name)
	}
	if cents != 18900 {
		t.Errorf("price_cents = %d, want 18900", cents)
	}
	if len(sizes) != 2 || sizes[0] != "P" {
		t.Errorf("sizes = %v", sizes)
	}
	if len(details) != 1 {
		t.Errorf("details = %v", details)
	}

	// The trigger must bump updated_at without anyone writing the column.
	// Each Exec is its own transaction, and now() is the transaction start
	// time — which is why updated_at moves here and would not inside a
	// single transaction.
	time.Sleep(50 * time.Millisecond)
	if _, err := pool.Exec(ctx,
		`UPDATE products SET price_cents = 19900 WHERE id = $1`, id); err != nil {
		t.Fatalf("updating: %v", err)
	}

	var newUpdatedAt time.Time
	if err := pool.QueryRow(ctx,
		`SELECT updated_at FROM products WHERE id = $1`, id).Scan(&newUpdatedAt); err != nil {
		t.Fatalf("re-reading: %v", err)
	}
	if !newUpdatedAt.After(updatedAt) {
		t.Errorf("trigger did not bump updated_at: before %v, after %v", updatedAt, newUpdatedAt)
	}
}

func TestSiteStateIsSingleRow(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM site_state`).Scan(&count); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if count != 1 {
		t.Fatalf("site_state has %d rows, want 1", count)
	}

	// The boolean primary key with a CHECK must reject a second row.
	if _, err := pool.Exec(ctx, `INSERT INTO site_state (id) VALUES (FALSE)`); err == nil {
		_, _ = pool.Exec(ctx, `DELETE FROM site_state WHERE id = FALSE`)
		t.Error("a second site_state row was accepted; it should be rejected")
	}
}
