package services

import (
	"context"
	"errors"
	"testing"

	"github.com/brendalima/venus-admin/internal/core/domain"
)

type mockUserRepository struct {
	users     map[string]domain.User // by email
	err       error
	createErr error
	created   []domain.User
}

func (m *mockUserRepository) Create(_ context.Context, u domain.User) (domain.User, error) {
	if m.createErr != nil {
		return domain.User{}, m.createErr
	}
	m.created = append(m.created, u)
	return u, nil
}

func (m *mockUserRepository) GetByID(_ context.Context, id string) (domain.User, error) {
	if m.err != nil {
		return domain.User{}, m.err
	}
	for _, u := range m.users {
		if u.ID == id {
			return u, nil
		}
	}
	return domain.User{}, domain.NewNotFoundError("user not found")
}

func (m *mockUserRepository) GetByEmail(_ context.Context, email string) (domain.User, error) {
	if m.err != nil {
		return domain.User{}, m.err
	}
	u, ok := m.users[email]
	if !ok {
		return domain.User{}, domain.NewNotFoundError("user not found")
	}
	return u, nil
}

type mockSessionRepository struct {
	calls int
	err   error
}

func (m *mockSessionRepository) DeleteExpired(context.Context) (int64, error) {
	m.calls++
	return 0, m.err
}

func TestNewUser(t *testing.T) {
	if _, err := NewUser(nil, &mockSessionRepository{}); err == nil {
		t.Error("a nil user repository should be rejected")
	}
	if _, err := NewUser(&mockUserRepository{}, nil); err == nil {
		t.Error("a nil session repository should be rejected")
	}
}

func TestUserCreate(t *testing.T) {
	tests := []struct {
		name      string
		email     string
		password  string
		createErr error
		wantField string // validation error expected on this field
		wantErr   error
	}{
		{name: "valid", email: " Ana@Exemplo.com ", password: "uma senha longa"},
		{name: "bad email", email: "ana", password: "uma senha longa", wantField: "email"},
		{name: "short password", email: "ana@exemplo.com", password: "curta", wantField: "password"},
		{
			name: "email taken", email: "ana@exemplo.com", password: "uma senha longa",
			createErr: domain.NewConflictError("email already in use"),
			wantErr:   &domain.ConflictError{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &mockUserRepository{createErr: tt.createErr}
			s := MustNewUser(repo, &mockSessionRepository{})

			u, err := s.Create(context.Background(), tt.email, tt.password)

			if tt.wantField != "" {
				var v *domain.ValidationError
				if !errors.As(err, &v) || v.Field != tt.wantField {
					t.Fatalf("err = %v, want a ValidationError on %s", err, tt.wantField)
				}
				if len(repo.created) != 0 {
					t.Error("an invalid user reached the repository")
				}
				return
			}
			if tt.wantErr != nil {
				var conflict *domain.ConflictError
				if !errors.As(err, &conflict) {
					t.Fatalf("err = %v, want ConflictError", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if u.Email != "ana@exemplo.com" {
				t.Errorf("email = %q, want it normalised", u.Email)
			}
			if u.ID == "" {
				t.Error("no ID was generated")
			}
			if u.PasswordHash == "" || u.PasswordHash == tt.password {
				t.Errorf("password hash = %q", u.PasswordHash)
			}
			if ok, _ := verifyPassword(tt.password, u.PasswordHash); !ok {
				t.Error("the stored hash does not verify against the password")
			}
		})
	}
}

func TestUserAuthenticate(t *testing.T) {
	hash, err := hashPassword("uma senha longa")
	if err != nil {
		t.Fatal(err)
	}
	ana := domain.User{ID: "u1", Email: "ana@exemplo.com", PasswordHash: hash}

	tests := []struct {
		name         string
		email        string
		password     string
		repoErr      error
		users        map[string]domain.User
		wantID       string
		unauthorized bool
	}{
		{name: "right password", email: "ana@exemplo.com", password: "uma senha longa", wantID: "u1"},
		{name: "email in another case", email: " ANA@exemplo.com", password: "uma senha longa", wantID: "u1"},
		{name: "wrong password", email: "ana@exemplo.com", password: "outra senha", unauthorized: true},
		{name: "unknown email", email: "bia@exemplo.com", password: "uma senha longa", unauthorized: true},
		{name: "repository down", email: "ana@exemplo.com", password: "uma senha longa", repoErr: errors.New("timeout")},
		{
			name: "corrupt hash", email: "ana@exemplo.com", password: "uma senha longa",
			users: map[string]domain.User{"ana@exemplo.com": {ID: "u1", Email: "ana@exemplo.com", PasswordHash: "garbage"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			users := tt.users
			if users == nil {
				users = map[string]domain.User{ana.Email: ana}
			}
			sessions := &mockSessionRepository{}
			s := MustNewUser(&mockUserRepository{users: users, err: tt.repoErr}, sessions)

			u, err := s.Authenticate(context.Background(), tt.email, tt.password)

			var unauthorized *domain.UnauthorizedError
			switch {
			case tt.unauthorized:
				if !errors.As(err, &unauthorized) {
					t.Fatalf("err = %v, want UnauthorizedError", err)
				}
			case tt.wantID != "":
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if u.ID != tt.wantID {
					t.Errorf("user = %+v", u)
				}
				if sessions.calls != 1 {
					t.Errorf("expired sessions cleaned %d times, want 1", sessions.calls)
				}
			default:
				// Infrastructure failures must not be dressed up as a wrong
				// password: the owner would keep retrying a good one.
				if err == nil || errors.As(err, &unauthorized) {
					t.Fatalf("err = %v, want a non-authorization error", err)
				}
			}
		})
	}
}

func TestAuthenticateSurvivesSessionCleanupFailure(t *testing.T) {
	hash, _ := hashPassword("uma senha longa")
	repo := &mockUserRepository{users: map[string]domain.User{
		"ana@exemplo.com": {ID: "u1", Email: "ana@exemplo.com", PasswordHash: hash},
	}}
	s := MustNewUser(repo, &mockSessionRepository{err: errors.New("timeout")})

	if _, err := s.Authenticate(context.Background(), "ana@exemplo.com", "uma senha longa"); err != nil {
		t.Errorf("login failed because session cleanup did: %v", err)
	}
}
