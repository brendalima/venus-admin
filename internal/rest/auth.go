package rest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/alexedwards/scs/v2"

	"github.com/brendalima/venus-admin/internal/core/domain"
	"github.com/brendalima/venus-admin/internal/core/services"
)

// maxLoginFormBytes is far above any real email and password, and far
// below anything that would cost memory to parse.
const maxLoginFormBytes = 4 << 10

type loginPage struct {
	Email string
	Error string
}

type AuthInput struct {
	Service   services.UserService
	Sessions  *scs.SessionManager
	Limiter   *LoginLimiter
	Templates *Templates

	// ClientIPHeader names the header the proxy puts the client's IP in.
	// Empty means the server is reached directly.
	ClientIPHeader string
}

// Auth handles logging in and out, and guards the admin's pages.
type Auth struct {
	service        services.UserService
	sessions       *scs.SessionManager
	limiter        *LoginLimiter
	templates      *Templates
	clientIPHeader string
}

func NewAuth(in AuthInput) (*Auth, error) {
	switch {
	case in.Service == nil:
		return nil, errors.New("missing user service dependency")
	case in.Sessions == nil:
		return nil, errors.New("missing session manager dependency")
	case in.Limiter == nil:
		return nil, errors.New("missing login limiter dependency")
	case in.Templates == nil:
		return nil, errors.New("missing templates dependency")
	}
	return &Auth{
		service:        in.Service,
		sessions:       in.Sessions,
		limiter:        in.Limiter,
		templates:      in.Templates,
		clientIPHeader: in.ClientIPHeader,
	}, nil
}

func MustNewAuth(in AuthInput) *Auth {
	a, err := NewAuth(in)
	if err != nil {
		panic(fmt.Errorf("couldn't create auth handler: %w", err))
	}
	return a
}

func (a *Auth) LoginPage(w http.ResponseWriter, r *http.Request) {
	if a.sessions.GetString(r.Context(), sessionUserIDKey) != "" {
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}
	a.templates.render(w, r, http.StatusOK, "login.html", loginPage{})
}

func (a *Auth) Login(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Checked before anything else, the hash especially: this is what
	// keeps a password guesser from eating the machine's memory.
	ip := clientIP(r, a.clientIPHeader)
	if !a.limiter.Allow(ip) {
		slog.WarnContext(ctx, "login rate limited", "ip", ip)
		a.templates.render(w, r, http.StatusTooManyRequests, "login.html", loginPage{
			Error: "Muitas tentativas seguidas. Espere 15 minutos e tente de novo.",
		})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxLoginFormBytes)
	if err := r.ParseForm(); err != nil {
		a.templates.render(w, r, http.StatusBadRequest, "login.html", loginPage{
			Error: "Não consegui ler o formulário. Tente de novo.",
		})
		return
	}

	page := loginPage{Email: r.PostForm.Get("email")}
	password := r.PostForm.Get("password")
	if page.Email == "" || password == "" {
		page.Error = "Preencha o e-mail e a senha."
		a.templates.render(w, r, http.StatusBadRequest, "login.html", page)
		return
	}

	user, err := a.service.Authenticate(ctx, page.Email, password)
	var unauthorized *domain.UnauthorizedError
	if errors.As(err, &unauthorized) {
		slog.InfoContext(ctx, "login failed", "ip", ip)
		page.Error = "E-mail ou senha incorretos."
		a.templates.render(w, r, http.StatusUnauthorized, "login.html", page)
		return
	}
	if err != nil {
		slog.ErrorContext(ctx, "couldn't authenticate", "error", err)
		page.Error = "Não consegui entrar agora. Tente de novo em instantes."
		a.templates.render(w, r, http.StatusInternalServerError, "login.html", page)
		return
	}

	if err := startSession(ctx, a.sessions, user); err != nil {
		slog.ErrorContext(ctx, "couldn't start session", "error", err)
		page.Error = "Não consegui entrar agora. Tente de novo em instantes."
		a.templates.render(w, r, http.StatusInternalServerError, "login.html", page)
		return
	}
	a.limiter.Reset(ip)

	slog.InfoContext(ctx, "login", "user_id", user.ID)
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (a *Auth) Logout(w http.ResponseWriter, r *http.Request) {
	if err := a.sessions.Destroy(r.Context()); err != nil {
		slog.ErrorContext(r.Context(), "couldn't destroy session", "error", err)
		http.Error(w, "Erro interno", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

type userContextKey struct{}

// RequireLogin lets the request through only with a session whose user
// still exists, and makes that user available to the handler through
// currentUser. It must run inside the session manager's LoadAndSave.
func (a *Auth) RequireLogin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		id := a.sessions.GetString(ctx, sessionUserIDKey)
		if id == "" {
			redirectToLogin(w, r)
			return
		}

		user, err := a.service.GetByID(ctx, id)
		var notFound *domain.NotFoundError
		if errors.As(err, &notFound) {
			// The account was removed while the session lived on.
			if err := a.sessions.Destroy(ctx); err != nil {
				slog.ErrorContext(ctx, "couldn't destroy session", "error", err)
			}
			redirectToLogin(w, r)
			return
		}
		if err != nil {
			slog.ErrorContext(ctx, "couldn't load session user", "error", err)
			http.Error(w, "Erro interno", http.StatusInternalServerError)
			return
		}
		// The password changed after this session logged in, here or on
		// another device: whoever changed it wanted the old one to stop
		// working everywhere. Sessions from before password versions
		// existed read as 0, which is every user's version until a reset.
		if a.sessions.GetInt(ctx, sessionPasswordVersionKey) != user.PasswordVersion {
			slog.InfoContext(ctx, "session from an old password", "user_id", user.ID)
			if err := a.sessions.Destroy(ctx); err != nil {
				slog.ErrorContext(ctx, "couldn't destroy session", "error", err)
			}
			redirectToLogin(w, r)
			return
		}

		// Logged-in pages must not be served from the browser's cache
		// after logging out, nor by the back button.
		noStore(w)
		next.ServeHTTP(w, r.WithContext(context.WithValue(ctx, userContextKey{}, user)))
	})
}

// startSession logs user in on this request's session. The token is
// renewed on every login, so a session ID planted before it (session
// fixation) is worth nothing after.
func startSession(ctx context.Context, sessions *scs.SessionManager, user domain.User) error {
	if err := sessions.RenewToken(ctx); err != nil {
		return fmt.Errorf("couldn't renew session token: %w", err)
	}
	sessions.Put(ctx, sessionUserIDKey, user.ID)
	sessions.Put(ctx, sessionPasswordVersionKey, user.PasswordVersion)
	return nil
}

// redirectToLogin sends the browser to the login page. HTMX follows a
// plain redirect inside the fragment it requested, which would draw the
// login form in the middle of a page, so it gets HX-Redirect instead.
func redirectToLogin(w http.ResponseWriter, r *http.Request) {
	if isHTMX(r) {
		w.Header().Set("HX-Redirect", "/login")
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// currentUser returns the user RequireLogin put in the context.
func currentUser(ctx context.Context) (domain.User, bool) {
	u, ok := ctx.Value(userContextKey{}).(domain.User)
	return u, ok
}
