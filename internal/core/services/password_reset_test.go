package services

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/brendalima/venus-admin/internal/core/domain"
)

type mockPasswordResetRepository struct {
	resets     map[string]string // user ID by token hash
	recent     int
	err        error
	consumeErr error
	ttl        time.Duration
	consumed   []string // password hashes set
}

func newMockPasswordResetRepository() *mockPasswordResetRepository {
	return &mockPasswordResetRepository{resets: map[string]string{}}
}

func (m *mockPasswordResetRepository) Create(_ context.Context, hash []byte, userID string, ttl time.Duration) error {
	if m.err != nil {
		return m.err
	}
	m.resets[string(hash)] = userID
	m.ttl = ttl
	return nil
}

func (m *mockPasswordResetRepository) CountRecent(context.Context, string, time.Duration) (int, error) {
	return m.recent, m.err
}

func (m *mockPasswordResetRepository) GetUserID(_ context.Context, hash []byte) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	id, ok := m.resets[string(hash)]
	if !ok {
		return "", domain.NewNotFoundError("password reset not found")
	}
	return id, nil
}

func (m *mockPasswordResetRepository) Consume(_ context.Context, hash []byte, passwordHash string) (domain.User, error) {
	if m.consumeErr != nil {
		return domain.User{}, m.consumeErr
	}
	id, ok := m.resets[string(hash)]
	if !ok {
		return domain.User{}, domain.NewNotFoundError("password reset not found")
	}
	delete(m.resets, string(hash))
	m.consumed = append(m.consumed, passwordHash)
	return domain.User{ID: id, PasswordHash: passwordHash, PasswordVersion: 1}, nil
}

type mockMailer struct {
	sent []domain.Email
	err  error
}

func (m *mockMailer) Send(_ context.Context, e domain.Email) error {
	if m.err != nil {
		return m.err
	}
	m.sent = append(m.sent, e)
	return nil
}

const testBaseURL = "https://admin.example.com"

func newTestPasswordReset() (*PasswordReset, *mockPasswordResetRepository, *mockMailer) {
	users := &mockUserRepository{users: map[string]domain.User{
		"ana@exemplo.com": {ID: "u1", Email: "ana@exemplo.com"},
	}}
	resets := newMockPasswordResetRepository()
	mailer := &mockMailer{}
	s := MustNewPasswordReset(PasswordResetInput{Users: users, Resets: resets, Mailer: mailer, BaseURL: testBaseURL})
	return s, resets, mailer
}

// tokenFrom pulls the token out of the link in a sent email.
func tokenFrom(t *testing.T, e domain.Email) string {
	t.Helper()
	for _, line := range strings.Split(e.Text, "\n") {
		if !strings.HasPrefix(line, testBaseURL+"/") {
			continue
		}
		u, err := url.Parse(line)
		if err != nil {
			t.Fatalf("link %q: %v", line, err)
		}
		if u.Path != "/reset-password" {
			t.Errorf("link path = %q, want /reset-password", u.Path)
		}
		return u.Query().Get("token")
	}
	t.Fatalf("no link in the email:\n%s", e.Text)
	return ""
}

func TestNewPasswordResetRejectsMissingDependencies(t *testing.T) {
	full := PasswordResetInput{
		Users: &mockUserRepository{}, Resets: newMockPasswordResetRepository(), Mailer: &mockMailer{}, BaseURL: testBaseURL,
	}
	for name, in := range map[string]PasswordResetInput{
		"users":    {Resets: full.Resets, Mailer: full.Mailer, BaseURL: full.BaseURL},
		"resets":   {Users: full.Users, Mailer: full.Mailer, BaseURL: full.BaseURL},
		"mailer":   {Users: full.Users, Resets: full.Resets, BaseURL: full.BaseURL},
		"base url": {Users: full.Users, Resets: full.Resets, Mailer: full.Mailer},
	} {
		if _, err := NewPasswordReset(in); err == nil {
			t.Errorf("missing %s was accepted", name)
		}
	}
	if _, err := NewPasswordReset(full); err != nil {
		t.Errorf("complete input rejected: %v", err)
	}
}

func TestPasswordResetRequest(t *testing.T) {
	tests := []struct {
		name     string
		email    string
		recent   int
		repoErr  error
		mailErr  error
		wantSent bool
		wantErr  bool
	}{
		{name: "known email", email: " Ana@Exemplo.com ", wantSent: true},
		{name: "unknown email", email: "bia@exemplo.com"},
		{name: "limit reached", email: "ana@exemplo.com", recent: maxPasswordResetsPerHour},
		{name: "just under the limit", email: "ana@exemplo.com", recent: maxPasswordResetsPerHour - 1, wantSent: true},
		{name: "database down", email: "ana@exemplo.com", repoErr: errors.New("connection refused"), wantErr: true},
		{name: "email not sent", email: "ana@exemplo.com", mailErr: errors.New("resend answered 403"), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, resets, mailer := newTestPasswordReset()
			resets.recent, resets.err, mailer.err = tt.recent, tt.repoErr, tt.mailErr

			err := s.Request(context.Background(), tt.email)

			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, want error: %v", err, tt.wantErr)
			}
			if got := len(mailer.sent) == 1; got != tt.wantSent {
				t.Fatalf("sent %d emails, want sent: %v", len(mailer.sent), tt.wantSent)
			}
			if !tt.wantSent {
				return
			}

			e := mailer.sent[0]
			if e.To != "ana@exemplo.com" {
				t.Errorf("sent to %q", e.To)
			}
			token := tokenFrom(t, e)
			if len(resets.resets) != 1 || resets.ttl != PasswordResetTTL {
				t.Fatalf("stored %d resets with ttl %v", len(resets.resets), resets.ttl)
			}
			for hash := range resets.resets {
				if hash == token {
					t.Error("the token itself was stored instead of its hash")
				}
			}
			if err := s.Check(context.Background(), token); err != nil {
				t.Errorf("the emailed token does not check out: %v", err)
			}
		})
	}
}

func TestPasswordResetTokensAreUnique(t *testing.T) {
	s, _, mailer := newTestPasswordReset()
	for range 2 {
		if err := s.Request(context.Background(), "ana@exemplo.com"); err != nil {
			t.Fatal(err)
		}
	}
	if a, b := tokenFrom(t, mailer.sent[0]), tokenFrom(t, mailer.sent[1]); a == b {
		t.Errorf("two requests got the same token %q", a)
	}
}

func TestPasswordResetCheck(t *testing.T) {
	s, _, mailer := newTestPasswordReset()
	if err := s.Request(context.Background(), "ana@exemplo.com"); err != nil {
		t.Fatal(err)
	}
	valid := tokenFrom(t, mailer.sent[0])
	other, _, err := newResetToken()
	if err != nil {
		t.Fatal(err)
	}

	for name, token := range map[string]string{
		"unknown":    other,
		"empty":      "",
		"not base64": "não é um token",
		"too short":  valid[:20],
	} {
		var notFound *domain.NotFoundError
		if err := s.Check(context.Background(), token); !errors.As(err, &notFound) {
			t.Errorf("%s: err = %v, want NotFoundError", name, err)
		}
	}
}

func TestPasswordResetReset(t *testing.T) {
	tests := []struct {
		name         string
		password     string
		badToken     bool
		consumeErr   error
		wantField    string
		wantNotFound bool
		wantErr      bool // any other error
	}{
		{name: "valid", password: "uma senha nova e longa"},
		{name: "short password", password: "curta", wantField: "password"},
		{name: "dead link", password: "uma senha nova e longa", badToken: true, wantNotFound: true},
		{
			name: "used by someone else meanwhile", password: "uma senha nova e longa",
			consumeErr: domain.NewNotFoundError("password reset not found"), wantNotFound: true,
		},
		{name: "database down", password: "uma senha nova e longa", consumeErr: errors.New("connection refused"), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, resets, mailer := newTestPasswordReset()
			if err := s.Request(context.Background(), "ana@exemplo.com"); err != nil {
				t.Fatal(err)
			}
			token := tokenFrom(t, mailer.sent[0])
			if tt.badToken {
				token, _, _ = newResetToken()
			}
			resets.consumeErr = tt.consumeErr

			u, err := s.Reset(context.Background(), token, tt.password)

			if tt.wantField != "" {
				var v *domain.ValidationError
				if !errors.As(err, &v) || v.Field != tt.wantField {
					t.Fatalf("err = %v, want a ValidationError on %s", err, tt.wantField)
				}
				if len(resets.consumed) != 0 {
					t.Error("an invalid password used the link up")
				}
				return
			}
			if tt.wantNotFound {
				var notFound *domain.NotFoundError
				if !errors.As(err, &notFound) {
					t.Fatalf("err = %v, want NotFoundError", err)
				}
				return
			}
			if tt.wantErr {
				var notFound *domain.NotFoundError
				if err == nil || errors.As(err, &notFound) {
					t.Fatalf("err = %v, want a failure other than NotFoundError", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if u.ID != "u1" || u.PasswordVersion != 1 {
				t.Errorf("user = %+v", u)
			}
			if ok, _ := verifyPassword(tt.password, resets.consumed[0]); !ok {
				t.Error("the stored hash does not verify against the new password")
			}
			var notFound *domain.NotFoundError
			if err := s.Check(context.Background(), token); !errors.As(err, &notFound) {
				t.Errorf("the link still works after use: %v", err)
			}
		})
	}
}
