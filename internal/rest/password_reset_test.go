package rest

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2/memstore"

	"github.com/brendalima/venus-admin/internal/core/domain"
)

const testResetToken = "valid-token"

// mockPasswordResetService knows one live token, for the test user. A
// reset bumps that user's password version in users, as the real one
// does in the database.
type mockPasswordResetService struct {
	users      *mockUserService
	live       bool
	requested  []string
	requestErr error
	checkErr   error
}

func newMockPasswordResetService(users *mockUserService) *mockPasswordResetService {
	return &mockPasswordResetService{users: users, live: true}
}

func (m *mockPasswordResetService) Request(_ context.Context, email string) error {
	if m.requestErr != nil {
		return m.requestErr
	}
	m.requested = append(m.requested, email)
	return nil
}

func (m *mockPasswordResetService) Check(_ context.Context, token string) error {
	if m.checkErr != nil {
		return m.checkErr
	}
	if !m.live || token != testResetToken {
		return domain.NewNotFoundError("password reset not found")
	}
	return nil
}

func (m *mockPasswordResetService) Reset(ctx context.Context, token, password string) (domain.User, error) {
	if err := domain.ValidatePassword(password); err != nil {
		return domain.User{}, err
	}
	if err := m.Check(ctx, token); err != nil {
		return domain.User{}, err
	}
	m.live = false
	u := m.users.users["u1"]
	u.PasswordVersion++
	m.users.users["u1"] = u
	return u, nil
}

func TestNewPasswordResetRejectsMissingDependencies(t *testing.T) {
	full := PasswordResetInput{
		Service:   newMockPasswordResetService(newMockUserService()),
		Sessions:  MustNewSessionManager(memstore.NewWithCleanupInterval(0), false),
		Limiter:   NewLoginLimiter(1, time.Minute),
		Templates: MustNewTemplates(testImageBaseURL),
	}
	for name, in := range map[string]PasswordResetInput{
		"service":   {Sessions: full.Sessions, Limiter: full.Limiter, Templates: full.Templates},
		"sessions":  {Service: full.Service, Limiter: full.Limiter, Templates: full.Templates},
		"limiter":   {Service: full.Service, Sessions: full.Sessions, Templates: full.Templates},
		"templates": {Service: full.Service, Sessions: full.Sessions, Limiter: full.Limiter},
	} {
		if _, err := NewPasswordReset(in); err == nil {
			t.Errorf("missing %s was accepted", name)
		}
	}
	if _, err := NewPasswordReset(full); err != nil {
		t.Errorf("complete input rejected: %v", err)
	}
}

func TestLoginPageLinksToPasswordReset(t *testing.T) {
	app := newTestApp(t, nil)
	if _, body := app.get(t, "/login", nil); !strings.Contains(body, `href="/forgot-password"`) {
		t.Error("the login page has no way to reset the password")
	}
}

func TestForgotPassword(t *testing.T) {
	tests := []struct {
		name       string
		email      string
		requestErr error
		wantStatus int
		wantText   string
		wantAsked  bool
	}{
		{name: "sent", email: " Ana@Exemplo.com ", wantStatus: http.StatusSeeOther, wantAsked: true},
		{name: "empty", email: "", wantStatus: http.StatusBadRequest, wantText: "Falta o e-mail"},
		{name: "typo", email: "ana@exemplo", wantStatus: http.StatusBadRequest, wantText: "Esse e-mail não parece certo"},
		{
			name: "email not sent", email: testEmail, requestErr: errors.New("resend answered 403"),
			wantStatus: http.StatusInternalServerError, wantText: "Não consegui enviar o e-mail agora",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := newTestApp(t, nil)
			app.resets.requestErr = tt.requestErr

			res, body := app.post(t, "/forgot-password", url.Values{"email": {tt.email}})

			if res.StatusCode != tt.wantStatus {
				t.Fatalf("status %d, want %d", res.StatusCode, tt.wantStatus)
			}
			if !strings.Contains(body, tt.wantText) {
				t.Errorf("body does not contain %q", tt.wantText)
			}
			if asked := len(app.resets.requested) == 1; asked != tt.wantAsked {
				t.Errorf("service asked %d times, want asked: %v", len(app.resets.requested), tt.wantAsked)
			}
			if tt.wantStatus == http.StatusSeeOther && res.Header.Get("Location") != "/forgot-password" {
				t.Errorf("location %q, want /forgot-password", res.Header.Get("Location"))
			}
		})
	}
}

func TestForgotPasswordConfirmationShowsOnce(t *testing.T) {
	app := newTestApp(t, nil)
	app.post(t, "/forgot-password", url.Values{"email": {" Ana@Exemplo.com "}})

	_, body := app.get(t, "/forgot-password", nil)
	if !strings.Contains(body, "Confira seu e-mail") || !strings.Contains(body, testEmail) {
		t.Error("the confirmation does not name the address")
	}
	// Reloading later shows the form again, not a stale confirmation.
	if _, body := app.get(t, "/forgot-password", nil); !strings.Contains(body, `name="email"`) {
		t.Error("the confirmation stayed after being shown")
	}
}

func TestForgotPasswordIsRateLimited(t *testing.T) {
	app := newTestApp(t, NewLoginLimiter(2, time.Minute))
	for range 2 {
		app.post(t, "/forgot-password", url.Values{"email": {testEmail}})
	}
	res, body := app.post(t, "/forgot-password", url.Values{"email": {testEmail}})
	if res.StatusCode != http.StatusTooManyRequests || !strings.Contains(body, "Espere 15 minutos") {
		t.Errorf("third request: status %d, want 429 with a message", res.StatusCode)
	}
	if len(app.resets.requested) != 2 {
		t.Errorf("service asked %d times, want 2", len(app.resets.requested))
	}
}

func TestResetPasswordPage(t *testing.T) {
	tests := []struct {
		name       string
		token      string
		checkErr   error
		wantStatus int
		wantText   string
	}{
		{name: "live link", token: testResetToken, wantStatus: http.StatusOK, wantText: `value="` + testResetToken + `"`},
		{name: "dead link", token: "other", wantStatus: http.StatusNotFound, wantText: "Esse link não vale mais"},
		{name: "no token", wantStatus: http.StatusNotFound, wantText: "Esse link não vale mais"},
		{
			name: "database down", token: testResetToken, checkErr: errors.New("connection refused"),
			wantStatus: http.StatusInternalServerError, wantText: "Não consegui abrir o link agora",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := newTestApp(t, nil)
			app.resets.checkErr = tt.checkErr

			res, body := app.get(t, "/reset-password?token="+url.QueryEscape(tt.token), nil)

			if res.StatusCode != tt.wantStatus {
				t.Fatalf("status %d, want %d", res.StatusCode, tt.wantStatus)
			}
			if !strings.Contains(body, tt.wantText) {
				t.Errorf("body does not contain %q", tt.wantText)
			}
			if res.Header.Get("Cache-Control") != "no-store" {
				t.Errorf("Cache-Control = %q, want no-store", res.Header.Get("Cache-Control"))
			}
		})
	}
}

func TestResetPassword(t *testing.T) {
	tests := []struct {
		name       string
		form       url.Values
		wantStatus int
		wantText   string
	}{
		{
			name:       "passwords differ",
			form:       url.Values{"token": {testResetToken}, "password": {"uma senha nova"}, "confirm": {"uma senha nva"}},
			wantStatus: http.StatusBadRequest, wantText: "As duas senhas não são iguais",
		},
		{
			name:       "too short",
			form:       url.Values{"token": {testResetToken}, "password": {"curta"}, "confirm": {"curta"}},
			wantStatus: http.StatusBadRequest, wantText: "pelo menos 8 caracteres",
		},
		{
			name:       "dead link",
			form:       url.Values{"token": {"other"}, "password": {"uma senha nova"}, "confirm": {"uma senha nova"}},
			wantStatus: http.StatusNotFound, wantText: "Esse link não vale mais",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := newTestApp(t, nil)

			res, body := app.post(t, "/reset-password", tt.form)

			if res.StatusCode != tt.wantStatus {
				t.Fatalf("status %d, want %d", res.StatusCode, tt.wantStatus)
			}
			if !strings.Contains(body, tt.wantText) {
				t.Errorf("body does not contain %q", tt.wantText)
			}
			if tt.wantStatus == http.StatusBadRequest && !strings.Contains(body, `value="`+testResetToken+`"`) {
				t.Error("a rejected form lost the token, so the person cannot try again")
			}
			if !app.resets.live {
				t.Error("a rejected form used the link up")
			}
		})
	}
}

func TestResetPasswordLogsInAndOutOthers(t *testing.T) {
	app := newTestApp(t, nil)
	// Another device, logged in with the old password.
	other := app.withNewDevice(t)
	other.login(t)

	res, _ := app.post(t, "/reset-password", url.Values{
		"token": {testResetToken}, "password": {"uma senha nova"}, "confirm": {"uma senha nova"},
	})
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/admin" {
		t.Fatalf("reset: status %d, location %q, want 303 to /admin", res.StatusCode, res.Header.Get("Location"))
	}

	res, body := app.get(t, "/admin", nil)
	if res.StatusCode != http.StatusOK || !strings.Contains(body, "Senha nova salva") {
		t.Errorf("/admin after reset: status %d, want the admin with a confirmation", res.StatusCode)
	}

	if res, _ := other.get(t, "/admin", nil); res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/login" {
		t.Errorf("other device after reset: status %d, location %q, want 303 to /login",
			res.StatusCode, res.Header.Get("Location"))
	}

	// The link is spent.
	if res, _ := app.get(t, "/reset-password?token="+testResetToken, nil); res.StatusCode != http.StatusNotFound {
		t.Errorf("link after use: status %d, want 404", res.StatusCode)
	}
}

func TestOldPasswordSessionIsLoggedOut(t *testing.T) {
	app := newTestApp(t, nil)
	app.login(t)

	// The password changed somewhere else.
	u := app.service.users["u1"]
	u.PasswordVersion++
	app.service.users["u1"] = u

	res, _ := app.get(t, "/admin", nil)
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/login" {
		t.Fatalf("/admin with an old session: status %d, location %q, want 303 to /login",
			res.StatusCode, res.Header.Get("Location"))
	}
	// Destroyed, not merely turned away once.
	u.PasswordVersion--
	app.service.users["u1"] = u
	if res, _ := app.get(t, "/admin", nil); res.StatusCode != http.StatusSeeOther {
		t.Errorf("the old session came back: status %d", res.StatusCode)
	}
}
