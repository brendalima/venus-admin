package rest

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2/memstore"

	"github.com/brendalima/venus-admin/internal/core/domain"
)

const (
	testEmail        = "ana@exemplo.com"
	testPassword     = "uma senha longa"
	testImageBaseURL = "https://fotos.example.com"
	testAPIToken     = "build-token-for-tests"
)

type mockUserService struct {
	users   map[string]domain.User // by ID
	authErr error
	getErr  error
}

func (m *mockUserService) Create(context.Context, string, string) (domain.User, error) {
	return domain.User{}, errors.New("not used by the handlers")
}

func (m *mockUserService) Authenticate(_ context.Context, email, password string) (domain.User, error) {
	if m.authErr != nil {
		return domain.User{}, m.authErr
	}
	for _, u := range m.users {
		if u.Email == email && password == testPassword {
			return u, nil
		}
	}
	return domain.User{}, domain.NewUnauthorizedError("invalid email or password")
}

func (m *mockUserService) GetByID(_ context.Context, id string) (domain.User, error) {
	if m.getErr != nil {
		return domain.User{}, m.getErr
	}
	u, ok := m.users[id]
	if !ok {
		return domain.User{}, domain.NewNotFoundError("user not found")
	}
	return u, nil
}

func newMockUserService() *mockUserService {
	return &mockUserService{users: map[string]domain.User{"u1": {ID: "u1", Email: testEmail}}}
}

// testApp wires the routes the way cmd/api does, so the tests exercise
// the session cookie and middleware for real.
type testApp struct {
	server   *httptest.Server
	client   *http.Client
	service  *mockUserService
	products *mockProductService
	site     *mockSiteService
	resets   *mockPasswordResetService
}

func newTestApp(t *testing.T, limiter *LoginLimiter) *testApp {
	t.Helper()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))

	if limiter == nil {
		limiter = NewLoginLimiter(100, time.Minute)
	}
	service := newMockUserService()
	templates := MustNewTemplates(testImageBaseURL)
	sessions := MustNewSessionManager(memstore.NewWithCleanupInterval(0), false)
	auth := MustNewAuth(AuthInput{
		Service:   service,
		Sessions:  sessions,
		Limiter:   limiter,
		Templates: templates,
	})
	products := newMockProductService()
	site := &mockSiteService{status: domain.SiteStatus{CanPublish: true}}
	product := MustNewProduct(ProductInput{Service: products, Site: site, Sessions: sessions, Templates: templates})
	api := MustNewAPI(APIInput{Service: products, Token: testAPIToken, ImageBaseURL: testImageBaseURL})
	resets := newMockPasswordResetService(service)
	// The same limiter caps both forms here; cmd/api gives each its own.
	reset := MustNewPasswordReset(PasswordResetInput{
		Service: resets, Sessions: sessions, Limiter: limiter, Templates: templates,
	})
	private := func(h http.HandlerFunc) http.Handler { return auth.RequireLogin(h) }

	app := http.NewServeMux()
	app.HandleFunc("GET /login", auth.LoginPage)
	app.HandleFunc("POST /login", auth.Login)
	app.HandleFunc("POST /logout", auth.Logout)
	app.HandleFunc("GET /forgot-password", reset.ForgotPage)
	app.HandleFunc("POST /forgot-password", reset.Forgot)
	app.HandleFunc("GET /reset-password", reset.ResetPage)
	app.HandleFunc("POST /reset-password", reset.Reset)
	app.Handle("GET /admin", private(product.List))
	app.Handle("GET /admin/products/new", private(product.New))
	app.Handle("POST /admin/products", private(product.Create))
	app.Handle("GET /admin/products/{id}", private(product.Edit))
	app.Handle("POST /admin/products/{id}", private(product.Update))
	app.Handle("POST /admin/products/{id}/sold-out", private(product.SetSoldOut))
	app.Handle("GET /admin/products/{id}/archive", private(product.ConfirmArchive))
	app.Handle("POST /admin/products/{id}/archive", private(product.Archive))
	app.Handle("POST /admin/products/{id}/unarchive", private(product.Unarchive))
	app.Handle("GET /admin/arquivadas", private(product.ListArchived))
	// Reached from /admin/arquivadas only: a piece leaves the collection
	// first, and is deleted for good from there.
	app.Handle("GET /admin/products/{id}/delete", private(product.ConfirmDelete))
	app.Handle("POST /admin/products/{id}/delete", private(product.Delete))
	app.Handle("POST /admin/publish", private(product.Publish))
	// Stands in for anything that stores data before login, giving the
	// visitor a real session token to carry into it.
	app.HandleFunc("GET /visit", func(_ http.ResponseWriter, r *http.Request) {
		sessions.Put(r.Context(), "visited", true)
	})

	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.StripPrefix("/static/", MustNewStatic()))
	mux.Handle("GET /api/products", api.RequireToken(http.HandlerFunc(api.Products)))
	mux.Handle("/", sessions.LoadAndSave(app))

	server := httptest.NewServer(RejectCrossOrigin(mux))
	t.Cleanup(server.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{
		Jar: jar,
		// Redirects are what is being tested; do not follow them.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return &testApp{server: server, client: client, service: service, products: products, site: site, resets: resets}
}

// withNewDevice returns the same app seen from another browser: same
// server and services, its own cookies.
func (a *testApp) withNewDevice(t *testing.T) *testApp {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	other := *a
	other.client = &http.Client{Jar: jar, CheckRedirect: a.client.CheckRedirect}
	return &other
}

func (a *testApp) get(t *testing.T, path string, header http.Header) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, a.server.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range header {
		req.Header[k] = v
	}
	return a.do(t, req)
}

func (a *testApp) post(t *testing.T, path string, form url.Values) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, a.server.URL+path, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return a.do(t, req)
}

func (a *testApp) login(t *testing.T) {
	t.Helper()
	res, _ := a.post(t, "/login", url.Values{"email": {testEmail}, "password": {testPassword}})
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/admin" {
		t.Fatalf("login: status %d, location %q", res.StatusCode, res.Header.Get("Location"))
	}
}

func (a *testApp) do(t *testing.T, req *http.Request) (*http.Response, string) {
	t.Helper()
	res, err := a.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res, string(body)
}

func TestNewAuthRejectsMissingDependencies(t *testing.T) {
	full := AuthInput{
		Service:   newMockUserService(),
		Sessions:  MustNewSessionManager(memstore.NewWithCleanupInterval(0), false),
		Limiter:   NewLoginLimiter(1, time.Minute),
		Templates: MustNewTemplates(testImageBaseURL),
	}
	for name, in := range map[string]AuthInput{
		"service":   {Sessions: full.Sessions, Limiter: full.Limiter, Templates: full.Templates},
		"sessions":  {Service: full.Service, Limiter: full.Limiter, Templates: full.Templates},
		"limiter":   {Service: full.Service, Sessions: full.Sessions, Templates: full.Templates},
		"templates": {Service: full.Service, Sessions: full.Sessions, Limiter: full.Limiter},
	} {
		if _, err := NewAuth(in); err == nil {
			t.Errorf("missing %s was accepted", name)
		}
	}
	if _, err := NewAuth(full); err != nil {
		t.Errorf("complete input rejected: %v", err)
	}
}

func TestAdminRequiresLogin(t *testing.T) {
	app := newTestApp(t, nil)

	res, _ := app.get(t, "/admin", nil)
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/login" {
		t.Errorf("anonymous /admin: status %d, location %q, want 303 to /login",
			res.StatusCode, res.Header.Get("Location"))
	}

	// HTMX would swap a redirected login page into the fragment.
	res, _ = app.get(t, "/admin", http.Header{"Hx-Request": {"true"}})
	if res.StatusCode != http.StatusUnauthorized || res.Header.Get("HX-Redirect") != "/login" {
		t.Errorf("anonymous HTMX /admin: status %d, HX-Redirect %q, want 401 with HX-Redirect",
			res.StatusCode, res.Header.Get("HX-Redirect"))
	}
}

func TestLoginLogout(t *testing.T) {
	app := newTestApp(t, nil)

	res, body := app.get(t, "/login", nil)
	if res.StatusCode != http.StatusOK || !strings.Contains(body, `name="password"`) {
		t.Fatalf("login page: status %d", res.StatusCode)
	}

	app.login(t)

	cookies := app.client.Jar.Cookies(mustParseURL(t, app.server.URL))
	if len(cookies) != 1 || cookies[0].Name != "venus_session" {
		t.Fatalf("cookies after login = %v", cookies)
	}

	res, body = app.get(t, "/admin", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("/admin after login: status %d", res.StatusCode)
	}
	if !strings.Contains(body, testEmail) {
		t.Error("/admin does not show who is logged in")
	}
	if res.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", res.Header.Get("Cache-Control"))
	}

	// Already logged in: the login page sends you onwards.
	if res, _ := app.get(t, "/login", nil); res.Header.Get("Location") != "/admin" {
		t.Errorf("logged-in /login: location %q, want /admin", res.Header.Get("Location"))
	}

	res, _ = app.post(t, "/logout", nil)
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/login" {
		t.Errorf("logout: status %d, location %q", res.StatusCode, res.Header.Get("Location"))
	}
	if res, _ := app.get(t, "/admin", nil); res.StatusCode != http.StatusSeeOther {
		t.Errorf("/admin after logout: status %d, want a redirect to login", res.StatusCode)
	}
}

func TestLoginRenewsSessionToken(t *testing.T) {
	app := newTestApp(t, nil)
	u := mustParseURL(t, app.server.URL)

	// A live session from before login, as a fixation attack would plant.
	// An unknown token would prove nothing: scs discards those by itself.
	app.get(t, "/visit", nil)
	before := sessionCookie(t, app, u)
	if before == "" {
		t.Fatal("no session before login")
	}

	app.login(t)

	if after := sessionCookie(t, app, u); after == before {
		t.Error("the session token did not change on login")
	}
}

func sessionCookie(t *testing.T, app *testApp, u *url.URL) string {
	t.Helper()
	for _, c := range app.client.Jar.Cookies(u) {
		if c.Name == "venus_session" {
			return c.Value
		}
	}
	return ""
}

func TestLoginFailures(t *testing.T) {
	tests := []struct {
		name       string
		form       url.Values
		authErr    error
		wantStatus int
		wantText   string
	}{
		{
			name:       "wrong password",
			form:       url.Values{"email": {testEmail}, "password": {"errada"}},
			wantStatus: http.StatusUnauthorized,
			wantText:   "E-mail ou senha incorretos.",
		},
		{
			name:       "missing password",
			form:       url.Values{"email": {testEmail}},
			wantStatus: http.StatusBadRequest,
			wantText:   "Preencha o e-mail e a senha.",
		},
		{
			name:       "service down",
			form:       url.Values{"email": {testEmail}, "password": {testPassword}},
			authErr:    errors.New("database timeout"),
			wantStatus: http.StatusInternalServerError,
			wantText:   "Tente de novo em instantes.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := newTestApp(t, nil)
			app.service.authErr = tt.authErr

			res, body := app.post(t, "/login", tt.form)
			if res.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", res.StatusCode, tt.wantStatus)
			}
			if !strings.Contains(body, tt.wantText) {
				t.Errorf("body does not contain %q", tt.wantText)
			}
			if strings.Contains(body, "database timeout") {
				t.Error("an internal error leaked into the page")
			}
			// The email stays filled in, so only the password is retyped.
			if email := tt.form.Get("email"); !strings.Contains(body, `value="`+email+`"`) {
				t.Error("the email field was not refilled")
			}
			if res, _ := app.get(t, "/admin", nil); res.StatusCode != http.StatusSeeOther {
				t.Error("a failed login left a usable session")
			}
		})
	}
}

func TestLoginIsRateLimited(t *testing.T) {
	app := newTestApp(t, NewLoginLimiter(3, time.Minute))
	wrong := url.Values{"email": {testEmail}, "password": {"errada"}}

	for i := range 3 {
		if res, _ := app.post(t, "/login", wrong); res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status %d, want 401", i+1, res.StatusCode)
		}
	}

	// Over the limit even the right password is refused, or the limit
	// would be no obstacle to guessing.
	right := url.Values{"email": {testEmail}, "password": {testPassword}}
	res, body := app.post(t, "/login", right)
	if res.StatusCode != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429", res.StatusCode)
	}
	if !strings.Contains(body, "Muitas tentativas") {
		t.Error("the page does not explain the wait")
	}
}

func TestSessionOfRemovedUserIsDropped(t *testing.T) {
	app := newTestApp(t, nil)
	app.login(t)

	delete(app.service.users, "u1")

	res, _ := app.get(t, "/admin", nil)
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/login" {
		t.Errorf("status %d, location %q, want a redirect to login", res.StatusCode, res.Header.Get("Location"))
	}
}

func TestSessionUserLookupFailure(t *testing.T) {
	app := newTestApp(t, nil)
	app.login(t)

	app.service.getErr = errors.New("database timeout")

	// A database hiccup must not log the person out.
	res, _ := app.get(t, "/admin", nil)
	if res.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", res.StatusCode)
	}
	app.service.getErr = nil
	if res, _ := app.get(t, "/admin", nil); res.StatusCode != http.StatusOK {
		t.Errorf("after recovery: status = %d, want the session to have survived", res.StatusCode)
	}
}

func TestCrossOriginPostIsRejected(t *testing.T) {
	app := newTestApp(t, nil)
	app.login(t)

	req, err := http.NewRequest(http.MethodPost, app.server.URL+"/logout", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.Header.Set("Origin", "https://evil.example")

	res, _ := app.do(t, req)
	if res.StatusCode != http.StatusForbidden {
		t.Errorf("cross-site logout: status %d, want 403", res.StatusCode)
	}
	if res, _ := app.get(t, "/admin", nil); res.StatusCode != http.StatusOK {
		t.Error("the cross-site request logged the user out")
	}
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
