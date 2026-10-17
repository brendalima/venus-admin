package repository

import (
	"context"
	"crypto/rand"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/brendalima/venus-admin/internal/core/domain"
)

func randomTokenHash(t *testing.T) []byte {
	t.Helper()
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestPasswordResetConsume(t *testing.T) {
	db := testDB(t)
	users := MustNewUser(db)
	resets := MustNewPasswordReset(db)
	ctx := context.Background()

	u, err := users.Create(ctx, newTestUser(t, db))
	if err != nil {
		t.Fatal(err)
	}
	if u.PasswordVersion != 0 {
		t.Errorf("new user has password version %d, want 0", u.PasswordVersion)
	}

	used, other := randomTokenHash(t), randomTokenHash(t)
	for _, h := range [][]byte{used, other} {
		if err := resets.Create(ctx, h, u.ID, time.Hour); err != nil {
			t.Fatalf("creating: %v", err)
		}
	}

	if n, err := resets.CountRecent(ctx, u.ID, time.Hour); err != nil || n != 2 {
		t.Errorf("CountRecent = %d, %v; want 2", n, err)
	}
	if id, err := resets.GetUserID(ctx, used); err != nil || id != u.ID {
		t.Fatalf("GetUserID = %q, %v; want %q", id, err, u.ID)
	}

	updated, err := resets.Consume(ctx, used, "$argon2id$new-hash")
	if err != nil {
		t.Fatalf("consuming: %v", err)
	}
	if updated.ID != u.ID || updated.PasswordHash != "$argon2id$new-hash" || updated.PasswordVersion != 1 {
		t.Errorf("after consume: %+v, want the new hash at version 1", updated)
	}
	stored, err := users.GetByID(ctx, u.ID)
	if err != nil || stored.PasswordHash != updated.PasswordHash || stored.PasswordVersion != 1 {
		t.Errorf("stored user = %+v, %v; want the new hash at version 1", stored, err)
	}

	// Single use, and the user's other link went with it.
	for name, h := range map[string][]byte{"used": used, "other": other} {
		var notFound *domain.NotFoundError
		if _, err := resets.GetUserID(ctx, h); !errors.As(err, &notFound) {
			t.Errorf("%s link after consume: err = %v, want NotFoundError", name, err)
		}
		if _, err := resets.Consume(ctx, h, "$argon2id$again"); !errors.As(err, &notFound) {
			t.Errorf("%s link consumed again: err = %v, want NotFoundError", name, err)
		}
	}
}

func TestPasswordResetExpired(t *testing.T) {
	db := testDB(t)
	users := MustNewUser(db)
	resets := MustNewPasswordReset(db)
	ctx := context.Background()

	u, err := users.Create(ctx, newTestUser(t, db))
	if err != nil {
		t.Fatal(err)
	}
	h := randomTokenHash(t)
	if err := resets.Create(ctx, h, u.ID, -time.Second); err != nil {
		t.Fatal(err)
	}

	var notFound *domain.NotFoundError
	if _, err := resets.GetUserID(ctx, h); !errors.As(err, &notFound) {
		t.Errorf("GetUserID on an expired link: err = %v, want NotFoundError", err)
	}
	if _, err := resets.Consume(ctx, h, "$argon2id$new-hash"); !errors.As(err, &notFound) {
		t.Errorf("Consume on an expired link: err = %v, want NotFoundError", err)
	}
	stored, err := users.GetByID(ctx, u.ID)
	if err != nil || stored.PasswordVersion != 0 {
		t.Errorf("an expired link changed the user: %+v, %v", stored, err)
	}
}

func TestPasswordResetConsumeRace(t *testing.T) {
	db := testDB(t)
	users := MustNewUser(db)
	resets := MustNewPasswordReset(db)
	ctx := context.Background()

	u, err := users.Create(ctx, newTestUser(t, db))
	if err != nil {
		t.Fatal(err)
	}
	h := randomTokenHash(t)
	if err := resets.Create(ctx, h, u.ID, time.Hour); err != nil {
		t.Fatal(err)
	}

	const racers = 4
	var wg sync.WaitGroup
	errs := make([]error, racers)
	for i := range racers {
		wg.Go(func() {
			_, errs[i] = resets.Consume(ctx, h, "$argon2id$racer")
		})
	}
	wg.Wait()

	won := 0
	for _, err := range errs {
		var notFound *domain.NotFoundError
		switch {
		case err == nil:
			won++
		case !errors.As(err, &notFound):
			t.Errorf("unexpected error: %v", err)
		}
	}
	if won != 1 {
		t.Errorf("%d requests used the same link, want exactly 1", won)
	}
	if stored, err := users.GetByID(ctx, u.ID); err != nil || stored.PasswordVersion != 1 {
		t.Errorf("password version = %d, %v; want 1", stored.PasswordVersion, err)
	}
}

func TestPasswordResetUnknownToken(t *testing.T) {
	db := testDB(t)
	resets := MustNewPasswordReset(db)

	var notFound *domain.NotFoundError
	if _, err := resets.GetUserID(context.Background(), randomTokenHash(t)); !errors.As(err, &notFound) {
		t.Errorf("err = %v, want NotFoundError", err)
	}
}
