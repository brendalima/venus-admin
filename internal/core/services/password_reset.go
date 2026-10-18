package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/brendalima/venus-admin/internal/core/domain"
	"github.com/brendalima/venus-admin/internal/repository"
	"github.com/brendalima/venus-admin/internal/storage"
)

var _ PasswordResetService = (*PasswordReset)(nil)

const (
	// PasswordResetTTL is how long a link works. An hour covers reading
	// the email later on the phone; much longer and a forgotten email in
	// an inbox stays a way in.
	PasswordResetTTL = time.Hour

	// At most this many links per account per hour, so the form cannot
	// be used to flood someone's inbox or spend the free sending quota.
	maxPasswordResetsPerHour = 3

	// 256 bits: guessing a live token is hopeless, so the link alone is
	// enough proof.
	resetTokenBytes = 32
)

var errResetNotFound = domain.NewNotFoundError("password reset not found")

type PasswordResetInput struct {
	Users  repository.UserRepository
	Resets repository.PasswordResetRepository
	Mailer storage.Mailer

	// BaseURL is the admin's public address, such as
	// https://venus-admin.fly.dev, that links in emails start with. It
	// comes from config and never from the request's Host header, which
	// whoever sends the request chooses: they could have the email point
	// at their own site and collect the token.
	BaseURL string
}

type PasswordReset struct {
	users   repository.UserRepository
	resets  repository.PasswordResetRepository
	mailer  storage.Mailer
	baseURL string
}

func NewPasswordReset(in PasswordResetInput) (*PasswordReset, error) {
	switch {
	case in.Users == nil:
		return nil, errors.New("missing user repository dependency")
	case in.Resets == nil:
		return nil, errors.New("missing password reset repository dependency")
	case in.Mailer == nil:
		return nil, errors.New("missing mailer dependency")
	case in.BaseURL == "":
		return nil, errors.New("missing base url")
	}
	return &PasswordReset{users: in.Users, resets: in.Resets, mailer: in.Mailer, baseURL: in.BaseURL}, nil
}

func MustNewPasswordReset(in PasswordResetInput) *PasswordReset {
	p, err := NewPasswordReset(in)
	if err != nil {
		panic(fmt.Errorf("couldn't create password reset service: %w", err))
	}
	return p
}

func (s *PasswordReset) Request(ctx context.Context, email string) error {
	u, err := s.users.GetByEmail(ctx, domain.NormalizeEmail(email))
	var notFound *domain.NotFoundError
	if errors.As(err, &notFound) {
		slog.InfoContext(ctx, "password reset asked for an unknown email")
		return nil
	}
	if err != nil {
		return fmt.Errorf("couldn't look up user: %w", err)
	}

	n, err := s.resets.CountRecent(ctx, u.ID, time.Hour)
	if err != nil {
		return fmt.Errorf("couldn't count password resets of user %s: %w", u.ID, err)
	}
	if n >= maxPasswordResetsPerHour {
		slog.WarnContext(ctx, "password reset limit reached", "user_id", u.ID)
		return nil
	}

	token, hash, err := newResetToken()
	if err != nil {
		return err
	}
	if err := s.resets.Create(ctx, hash, u.ID, PasswordResetTTL); err != nil {
		return fmt.Errorf("couldn't create password reset for user %s: %w", u.ID, err)
	}
	if err := s.mailer.Send(ctx, s.resetEmail(u.Email, token)); err != nil {
		return fmt.Errorf("couldn't send password reset email to user %s: %w", u.ID, err)
	}

	slog.InfoContext(ctx, "password reset sent", "user_id", u.ID)
	return nil
}

func (s *PasswordReset) Check(ctx context.Context, token string) error {
	hash, ok := hashResetToken(token)
	if !ok {
		return errResetNotFound
	}
	_, err := s.resets.GetUserID(ctx, hash)
	return err
}

func (s *PasswordReset) Reset(ctx context.Context, token, password string) (domain.User, error) {
	if err := domain.ValidatePassword(password); err != nil {
		return domain.User{}, err
	}

	// Checked before hashing, so a dead link costs a lookup and not an
	// argon2id hash.
	hash, ok := hashResetToken(token)
	if !ok {
		return domain.User{}, errResetNotFound
	}
	if _, err := s.resets.GetUserID(ctx, hash); err != nil {
		return domain.User{}, err
	}

	passwordHash, err := hashPassword(password)
	if err != nil {
		return domain.User{}, fmt.Errorf("couldn't hash password: %w", err)
	}
	u, err := s.resets.Consume(ctx, hash, passwordHash)
	if err != nil {
		return domain.User{}, err
	}

	slog.InfoContext(ctx, "password reset", "user_id", u.ID)
	return u, nil
}

func (s *PasswordReset) resetEmail(to, token string) domain.Email {
	link := s.baseURL + "/reset-password?token=" + token
	return domain.Email{
		To:      to,
		Subject: "Criar uma senha nova no painel da Vênus Oasis",
		Text: "Olá!\n\n" +
			"Alguém pediu uma senha nova para entrar no painel da Vênus Oasis com este e-mail.\n\n" +
			"Para criar a senha nova, abra este link:\n" +
			link + "\n\n" +
			"O link vale por 1 hora e só pode ser usado uma vez.\n\n" +
			"Se não foi você, pode ignorar este e-mail: a sua senha continua a mesma.\n",
	}
}

// newResetToken returns the token that goes in the link and the hash that
// goes in the database. Only the person holding the email has the token.
func newResetToken() (token string, hash []byte, err error) {
	b := make([]byte, resetTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", nil, fmt.Errorf("couldn't generate reset token: %w", err)
	}
	sum := sha256.Sum256(b)
	return base64.RawURLEncoding.EncodeToString(b), sum[:], nil
}

// hashResetToken reports false for anything newResetToken could not have
// produced, which then needs no trip to the database.
func hashResetToken(token string) ([]byte, bool) {
	b, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(b) != resetTokenBytes {
		return nil, false
	}
	sum := sha256.Sum256(b)
	return sum[:], true
}
