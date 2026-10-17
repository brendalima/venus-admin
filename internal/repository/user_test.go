package repository

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/brendalima/venus-admin/internal/core/domain"
)

// newTestUser returns a user with an email unique to this run, and
// removes it when the test ends.
func newTestUser(t *testing.T, db *sql.DB) domain.User {
	t.Helper()
	id := uuid.New().String()
	t.Cleanup(func() {
		// Best effort: a leftover row is recognisable by its email.
		_, _ = db.ExecContext(context.Background(), `DELETE FROM admin_users WHERE id = $1`, id)
	})
	return domain.User{
		ID:           id,
		Email:        "test-" + id + "@example.com",
		PasswordHash: "$argon2id$not-a-real-hash",
	}
}

func TestUserCreateAndGet(t *testing.T) {
	db := testDB(t)
	repo := MustNewUser(db)
	ctx := context.Background()

	created, err := repo.Create(ctx, newTestUser(t, db))
	if err != nil {
		t.Fatalf("creating: %v", err)
	}
	if created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Error("timestamps were not filled in")
	}

	byID, err := repo.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("getting by id: %v", err)
	}
	byEmail, err := repo.GetByEmail(ctx, created.Email)
	if err != nil {
		t.Fatalf("getting by email: %v", err)
	}
	for name, got := range map[string]domain.User{"by id": byID, "by email": byEmail} {
		if got.ID != created.ID || got.Email != created.Email || got.PasswordHash != created.PasswordHash {
			t.Errorf("%s: got %+v, want %+v", name, got, created)
		}
	}
}

func TestUserEmailConflict(t *testing.T) {
	db := testDB(t)
	repo := MustNewUser(db)
	ctx := context.Background()

	existing, err := repo.Create(ctx, newTestUser(t, db))
	if err != nil {
		t.Fatalf("creating: %v", err)
	}
	clash := newTestUser(t, db)
	clash.Email = existing.Email

	var conflict *domain.ConflictError
	if _, err := repo.Create(ctx, clash); !errors.As(err, &conflict) {
		t.Errorf("create with a used email: err = %v, want ConflictError", err)
	}
}

// The service normalises emails; the CHECK constraint is the backstop if
// some future path forgets to.
func TestUserEmailMustBeLowerCase(t *testing.T) {
	db := testDB(t)
	repo := MustNewUser(db)

	u := newTestUser(t, db)
	u.Email = "Test-" + u.ID + "@Example.com"
	if _, err := repo.Create(context.Background(), u); err == nil {
		t.Error("a mixed-case email was stored")
	}
}

func TestUserNotFound(t *testing.T) {
	db := testDB(t)
	repo := MustNewUser(db)
	ctx := context.Background()

	tests := []struct {
		name string
		call func() error
	}{
		{"unknown id", func() error { _, err := repo.GetByID(ctx, uuid.New().String()); return err }},
		{"malformed id", func() error { _, err := repo.GetByID(ctx, "1"); return err }},
		{"unknown email", func() error { _, err := repo.GetByEmail(ctx, "nobody-"+uuid.New().String()+"@example.com"); return err }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var notFound *domain.NotFoundError
			if err := tt.call(); !errors.As(err, &notFound) {
				t.Errorf("err = %v, want NotFoundError", err)
			}
		})
	}
}
