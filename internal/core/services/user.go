package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/brendalima/venus-admin/internal/core/domain"
	"github.com/brendalima/venus-admin/internal/repository"
)

var _ UserService = (*User)(nil)

type User struct {
	repo     repository.UserRepository
	sessions repository.SessionRepository
}

func NewUser(repo repository.UserRepository, sessions repository.SessionRepository) (*User, error) {
	if repo == nil {
		return nil, errors.New("missing user repository dependency")
	}
	if sessions == nil {
		return nil, errors.New("missing session repository dependency")
	}
	return &User{repo: repo, sessions: sessions}, nil
}

func MustNewUser(repo repository.UserRepository, sessions repository.SessionRepository) *User {
	u, err := NewUser(repo, sessions)
	if err != nil {
		panic(fmt.Errorf("couldn't create user service: %w", err))
	}
	return u
}

func (s *User) Create(ctx context.Context, email, password string) (domain.User, error) {
	email = domain.NormalizeEmail(email)
	if err := domain.ValidateEmail(email); err != nil {
		return domain.User{}, err
	}
	if err := domain.ValidatePassword(password); err != nil {
		return domain.User{}, err
	}

	hash, err := hashPassword(password)
	if err != nil {
		return domain.User{}, fmt.Errorf("couldn't hash password: %w", err)
	}

	u, err := s.repo.Create(ctx, domain.User{
		ID:           uuid.New().String(),
		Email:        email,
		PasswordHash: hash,
	})
	if err != nil {
		return domain.User{}, fmt.Errorf("couldn't create user: %w", err)
	}
	return u, nil
}

func (s *User) Authenticate(ctx context.Context, email, password string) (domain.User, error) {
	invalid := domain.NewUnauthorizedError("invalid email or password")

	u, err := s.repo.GetByEmail(ctx, domain.NormalizeEmail(email))
	var notFound *domain.NotFoundError
	if errors.As(err, &notFound) {
		// Same work as a wrong password, so timing does not reveal
		// which emails have an account. The result is irrelevant.
		_, _ = verifyPassword(password, dummyHash())
		return domain.User{}, invalid
	}
	if err != nil {
		return domain.User{}, fmt.Errorf("couldn't look up user: %w", err)
	}

	ok, err := verifyPassword(password, u.PasswordHash)
	if err != nil {
		return domain.User{}, fmt.Errorf("couldn't verify password of user %s: %w", u.ID, err)
	}
	if !ok {
		return domain.User{}, invalid
	}

	s.deleteExpiredSessions(ctx)
	return u, nil
}

func (s *User) GetByID(ctx context.Context, id string) (domain.User, error) {
	return s.repo.GetByID(ctx, id)
}

// deleteExpiredSessions tidies the session table. A login is the right
// moment: someone is using the admin, so the database is awake anyway,
// and nothing has to wake it on a timer. A failure only leaves rows that
// can no longer be used, so it does not block the login.
func (s *User) deleteExpiredSessions(ctx context.Context) {
	n, err := s.sessions.DeleteExpired(ctx)
	if err != nil {
		slog.WarnContext(ctx, "couldn't delete expired sessions", "error", err)
		return
	}
	if n > 0 {
		slog.InfoContext(ctx, "deleted expired sessions", "count", n)
	}
}
