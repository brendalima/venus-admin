package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/brendalima/venus-admin/internal/core/domain"
)

var _ UserRepository = (*User)(nil)

const (
	userColumns = `id, email, password_hash, password_version, created_at, updated_at`

	adminUsersEmailKey = "admin_users_email_key"
)

type User struct {
	db *sql.DB
}

func NewUser(db *sql.DB) (*User, error) {
	if db == nil {
		return nil, errors.New("missing database dependency")
	}
	return &User{db: db}, nil
}

func MustNewUser(db *sql.DB) *User {
	u, err := NewUser(db)
	if err != nil {
		panic(fmt.Errorf("couldn't create user repository: %w", err))
	}
	return u
}

func (r *User) Create(ctx context.Context, u domain.User) (domain.User, error) {
	err := r.db.QueryRowContext(ctx,
		`INSERT INTO admin_users (id, email, password_hash)
		 VALUES ($1, $2, $3)
		 RETURNING password_version, created_at, updated_at`,
		u.ID, u.Email, u.PasswordHash,
	).Scan(&u.PasswordVersion, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation && pgErr.ConstraintName == adminUsersEmailKey {
			return domain.User{}, domain.NewConflictError(domain.ConflictEmail)
		}
		return domain.User{}, fmt.Errorf("couldn't create user: %w", err)
	}
	return u, nil
}

func (r *User) GetByID(ctx context.Context, id string) (domain.User, error) {
	if uuid.Validate(id) != nil {
		return domain.User{}, domain.NewNotFoundError("user not found")
	}
	return r.getOne(ctx, `SELECT `+userColumns+` FROM admin_users WHERE id = $1`, id)
}

func (r *User) GetByEmail(ctx context.Context, email string) (domain.User, error) {
	return r.getOne(ctx, `SELECT `+userColumns+` FROM admin_users WHERE email = $1`, email)
}

func (r *User) getOne(ctx context.Context, query string, arg any) (domain.User, error) {
	u, err := scanUser(r.db.QueryRowContext(ctx, query, arg))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.User{}, domain.NewNotFoundError("user not found")
	}
	if err != nil {
		return domain.User{}, fmt.Errorf("couldn't get user: %w", err)
	}
	return u, nil
}

// scanUser reads a row selected as userColumns.
func scanUser(row *sql.Row) (domain.User, error) {
	var u domain.User
	err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.PasswordVersion, &u.CreatedAt, &u.UpdatedAt)
	return u, err
}
