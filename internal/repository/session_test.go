package repository

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/google/uuid"
)

// The session manager only sees scs.CtxStore; this keeps the method set
// from drifting without anyone noticing until runtime.
var _ scs.CtxStore = (*Session)(nil)

func testToken(t *testing.T, db *sql.DB) string {
	t.Helper()
	token := "test-" + uuid.New().String()
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM sessions WHERE token = $1`, token)
	})
	return token
}

func TestSessionCommitFindDelete(t *testing.T) {
	db := testDB(t)
	repo := MustNewSession(db)
	ctx := context.Background()
	token := testToken(t, db)

	if _, found, err := repo.FindCtx(ctx, token); err != nil || found {
		t.Fatalf("unknown token: found = %v, err = %v", found, err)
	}

	if err := repo.CommitCtx(ctx, token, []byte("first"), time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("committing: %v", err)
	}
	// A second commit of the same token overwrites, as scs expects.
	if err := repo.CommitCtx(ctx, token, []byte("second"), time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("re-committing: %v", err)
	}

	data, found, err := repo.FindCtx(ctx, token)
	if err != nil || !found {
		t.Fatalf("finding: found = %v, err = %v", found, err)
	}
	if string(data) != "second" {
		t.Errorf("data = %q, want the last commit", data)
	}

	if err := repo.DeleteCtx(ctx, token); err != nil {
		t.Fatalf("deleting: %v", err)
	}
	if _, found, _ := repo.FindCtx(ctx, token); found {
		t.Error("deleted session is still found")
	}
	if err := repo.DeleteCtx(ctx, token); err != nil {
		t.Errorf("deleting a missing token should be a no-op, got %v", err)
	}
}

func TestSessionExpiry(t *testing.T) {
	db := testDB(t)
	repo := MustNewSession(db)
	ctx := context.Background()

	expired := testToken(t, db)
	live := testToken(t, db)
	if err := repo.CommitCtx(ctx, expired, []byte("old"), time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("committing expired: %v", err)
	}
	if err := repo.CommitCtx(ctx, live, []byte("new"), time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("committing live: %v", err)
	}

	if _, found, err := repo.FindCtx(ctx, expired); err != nil || found {
		t.Errorf("expired session: found = %v, err = %v, want not found", found, err)
	}

	n, err := repo.DeleteExpired(ctx)
	if err != nil {
		t.Fatalf("deleting expired: %v", err)
	}
	if n < 1 {
		t.Errorf("deleted %d sessions, want at least the expired one", n)
	}

	var remaining int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM sessions WHERE token = ANY($1)`, []string{expired, live},
	).Scan(&remaining); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if remaining != 1 {
		t.Errorf("%d of the two test sessions remain, want only the live one", remaining)
	}
}
