package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/brendalima/venus-admin/internal/core/domain"
)

var _ PasswordResetRepository = (*PasswordReset)(nil)

type PasswordReset struct {
	db *sql.DB
}

func NewPasswordReset(db *sql.DB) (*PasswordReset, error) {
	if db == nil {
		return nil, errors.New("missing database dependency")
	}
	return &PasswordReset{db: db}, nil
}

func MustNewPasswordReset(db *sql.DB) *PasswordReset {
	p, err := NewPasswordReset(db)
	if err != nil {
		panic(fmt.Errorf("couldn't create password reset repository: %w", err))
	}
	return p
}

func (r *PasswordReset) Create(ctx context.Context, tokenHash []byte, userID string, ttl time.Duration) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO password_resets (token_hash, user_id, expires_at)
		 VALUES ($1, $2, now() + $3::double precision * interval '1 second')`,
		tokenHash, userID, ttl.Seconds())
	if err != nil {
		return fmt.Errorf("couldn't create password reset: %w", err)
	}
	return nil
}

func (r *PasswordReset) CountRecent(ctx context.Context, userID string, window time.Duration) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx,
		`SELECT count(*) FROM password_resets
		 WHERE user_id = $1 AND created_at > now() - $2::double precision * interval '1 second'`,
		userID, window.Seconds(),
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("couldn't count password resets: %w", err)
	}
	return n, nil
}

func (r *PasswordReset) GetUserID(ctx context.Context, tokenHash []byte) (string, error) {
	var userID string
	err := r.db.QueryRowContext(ctx,
		`SELECT user_id FROM password_resets WHERE token_hash = $1 AND now() < expires_at`,
		tokenHash,
	).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", domain.NewNotFoundError("password reset not found")
	}
	if err != nil {
		return "", fmt.Errorf("couldn't get password reset: %w", err)
	}
	return userID, nil
}

func (r *PasswordReset) Consume(ctx context.Context, tokenHash []byte, passwordHash string) (domain.User, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.User{}, fmt.Errorf("couldn't begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Deleting is what claims the link: of two requests racing with the
	// same token, the second waits on the row lock and then finds nothing.
	var userID string
	err = tx.QueryRowContext(ctx,
		`DELETE FROM password_resets WHERE token_hash = $1 AND now() < expires_at RETURNING user_id`,
		tokenHash,
	).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.User{}, domain.NewNotFoundError("password reset not found")
	}
	if err != nil {
		return domain.User{}, fmt.Errorf("couldn't claim password reset: %w", err)
	}

	u, err := scanUser(tx.QueryRowContext(ctx,
		`UPDATE admin_users SET password_hash = $1, password_version = password_version + 1
		 WHERE id = $2
		 RETURNING `+userColumns,
		passwordHash, userID))
	if err != nil {
		return domain.User{}, fmt.Errorf("couldn't update password: %w", err)
	}

	// The user's other links would still work otherwise; expired ones of
	// anybody go too, since nothing else ever cleans them up.
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM password_resets WHERE user_id = $1 OR expires_at <= now()`, userID,
	); err != nil {
		return domain.User{}, fmt.Errorf("couldn't delete password resets: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return domain.User{}, fmt.Errorf("couldn't commit password reset: %w", err)
	}
	return u, nil
}
