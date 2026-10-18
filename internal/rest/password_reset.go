package rest

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/alexedwards/scs/v2"

	"github.com/brendalima/venus-admin/internal/core/domain"
	"github.com/brendalima/venus-admin/internal/core/services"
)

// sessionResetSentToKey carries the address a link went to across the
// redirect after the form, so the confirmation can name it.
const sessionResetSentToKey = "reset_sent_to"

type forgotPasswordPage struct {
	Email  string
	Error  string
	SentTo string // set once the form went through
}

type resetPasswordPage struct {
	// Token travels in the form rather than the action's query string,
	// so the POST's URL carries nothing worth stealing.
	Token   string
	Errors  map[string]string // by field
	Alert   string
	Expired bool // the link is unknown, used or past its hour
}

type PasswordResetInput struct {
	Service   services.PasswordResetService
	Sessions  *scs.SessionManager
	Limiter   *LoginLimiter
	Templates *Templates

	// ClientIPHeader is the same as AuthInput's.
	ClientIPHeader string
}

// PasswordReset handles "Esqueci minha senha": asking for the link, and
// setting the new password from it.
type PasswordReset struct {
	service        services.PasswordResetService
	sessions       *scs.SessionManager
	limiter        *LoginLimiter
	templates      *Templates
	clientIPHeader string
}

func NewPasswordReset(in PasswordResetInput) (*PasswordReset, error) {
	switch {
	case in.Service == nil:
		return nil, errors.New("missing password reset service dependency")
	case in.Sessions == nil:
		return nil, errors.New("missing session manager dependency")
	case in.Limiter == nil:
		return nil, errors.New("missing limiter dependency")
	case in.Templates == nil:
		return nil, errors.New("missing templates dependency")
	}
	return &PasswordReset{
		service:        in.Service,
		sessions:       in.Sessions,
		limiter:        in.Limiter,
		templates:      in.Templates,
		clientIPHeader: in.ClientIPHeader,
	}, nil
}

func MustNewPasswordReset(in PasswordResetInput) *PasswordReset {
	p, err := NewPasswordReset(in)
	if err != nil {
		panic(fmt.Errorf("couldn't create password reset handler: %w", err))
	}
	return p
}

func (h *PasswordReset) ForgotPage(w http.ResponseWriter, r *http.Request) {
	page := forgotPasswordPage{SentTo: h.sessions.PopString(r.Context(), sessionResetSentToKey)}
	h.templates.render(w, r, http.StatusOK, "forgot_password.html", page)
}

func (h *PasswordReset) Forgot(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Each request that reaches a real account sends an email, so the
	// form is capped per IP as well as per account.
	ip := clientIP(r, h.clientIPHeader)
	if !h.limiter.Allow(ip) {
		slog.WarnContext(ctx, "password reset rate limited", "ip", ip)
		h.templates.render(w, r, http.StatusTooManyRequests, "forgot_password.html", forgotPasswordPage{
			Error: "Muitos pedidos seguidos. Espere 15 minutos e tente de novo.",
		})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxLoginFormBytes)
	if err := r.ParseForm(); err != nil {
		h.templates.render(w, r, http.StatusBadRequest, "forgot_password.html", forgotPasswordPage{
			Error: "Não consegui ler o formulário. Tente de novo.",
		})
		return
	}

	page := forgotPasswordPage{Email: r.PostForm.Get("email")}
	// A typo is worth pointing out: otherwise the person waits for an
	// email that went nowhere.
	var invalid *domain.ValidationError
	if err := domain.ValidateEmail(domain.NormalizeEmail(page.Email)); errors.As(err, &invalid) {
		page.Error = invalid.Message
		h.templates.render(w, r, http.StatusBadRequest, "forgot_password.html", page)
		return
	}

	if err := h.service.Request(ctx, page.Email); err != nil {
		slog.ErrorContext(ctx, "couldn't send password reset", "error", err)
		page.Error = "Não consegui enviar o e-mail agora. Tente de novo em instantes."
		h.templates.render(w, r, http.StatusInternalServerError, "forgot_password.html", page)
		return
	}

	// Redirected, so reloading the confirmation does not send again.
	h.sessions.Put(ctx, sessionResetSentToKey, domain.NormalizeEmail(page.Email))
	http.Redirect(w, r, "/forgot-password", http.StatusSeeOther)
}

func (h *PasswordReset) ResetPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	noStore(w)

	token := r.URL.Query().Get("token")
	err := h.service.Check(ctx, token)
	var notFound *domain.NotFoundError
	if errors.As(err, &notFound) {
		h.templates.render(w, r, http.StatusNotFound, "reset_password.html", resetPasswordPage{Expired: true})
		return
	}
	if err != nil {
		slog.ErrorContext(ctx, "couldn't check password reset", "error", err)
		h.templates.render(w, r, http.StatusInternalServerError, "reset_password.html", resetPasswordPage{
			Token: token,
			Alert: "Não consegui abrir o link agora. Tente de novo em instantes.",
		})
		return
	}
	h.templates.render(w, r, http.StatusOK, "reset_password.html", resetPasswordPage{Token: token})
}

func (h *PasswordReset) Reset(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	noStore(w)

	r.Body = http.MaxBytesReader(w, r.Body, maxLoginFormBytes)
	if err := r.ParseForm(); err != nil {
		h.templates.render(w, r, http.StatusBadRequest, "reset_password.html", resetPasswordPage{
			Alert: "Não consegui ler o formulário. Abra o link do e-mail de novo.",
		})
		return
	}

	page := resetPasswordPage{Token: r.PostForm.Get("token"), Errors: map[string]string{}}
	password := r.PostForm.Get("password")
	if password != r.PostForm.Get("confirm") {
		page.Errors["confirm"] = "As duas senhas não são iguais. Digite a mesma senha nos dois campos."
		h.templates.render(w, r, http.StatusBadRequest, "reset_password.html", page)
		return
	}

	user, err := h.service.Reset(ctx, page.Token, password)
	var invalid *domain.ValidationError
	if errors.As(err, &invalid) {
		page.Errors[invalid.Field] = invalid.Message
		h.templates.render(w, r, http.StatusBadRequest, "reset_password.html", page)
		return
	}
	var notFound *domain.NotFoundError
	if errors.As(err, &notFound) {
		h.templates.render(w, r, http.StatusNotFound, "reset_password.html", resetPasswordPage{Expired: true})
		return
	}
	if err != nil {
		slog.ErrorContext(ctx, "couldn't reset password", "error", err)
		page.Alert = "Não consegui salvar a senha agora. Tente de novo em instantes."
		h.templates.render(w, r, http.StatusInternalServerError, "reset_password.html", page)
		return
	}

	// Straight into the admin: having just proven they hold the email,
	// asking for the new password again would only be one more step.
	if err := startSession(ctx, h.sessions, user); err != nil {
		slog.ErrorContext(ctx, "couldn't start session", "error", err)
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	h.sessions.Put(ctx, sessionFlashKey, "Senha nova salva. Use ela da próxima vez que entrar.")
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

// noStore keeps a page holding a reset token out of the browser's cache.
func noStore(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
}
