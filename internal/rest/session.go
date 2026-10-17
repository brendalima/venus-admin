package rest

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/alexedwards/scs/v2"
)

const sessionUserIDKey = "user_id"

// NewSessionManager configures the login session cookie.
//
// The owners use the admin from their own phones and should not have to
// log in every few days, so a session lasts 30 days but ends after 7
// without use. secure is false only in development, over plain HTTP.
func NewSessionManager(store scs.Store, secure bool) (*scs.SessionManager, error) {
	if store == nil {
		return nil, errors.New("missing session store dependency")
	}

	sm := scs.New()
	sm.Store = store
	sm.Lifetime = 30 * 24 * time.Hour
	sm.IdleTimeout = 7 * 24 * time.Hour
	// The table keeps a hash of the token, so a leaked dump or backup
	// does not hand out live sessions.
	sm.HashTokenInStore = true

	sm.Cookie.Name = "venus_session"
	sm.Cookie.HttpOnly = true
	sm.Cookie.Secure = secure
	sm.Cookie.SameSite = http.SameSiteLaxMode
	sm.Cookie.Path = "/"

	sm.ErrorFunc = func(w http.ResponseWriter, r *http.Request, err error) {
		slog.ErrorContext(r.Context(), "session store failed", "error", err, "path", r.URL.Path)
		http.Error(w, "Erro interno", http.StatusInternalServerError)
	}
	return sm, nil
}

func MustNewSessionManager(store scs.Store, secure bool) *scs.SessionManager {
	sm, err := NewSessionManager(store, secure)
	if err != nil {
		panic(fmt.Errorf("couldn't create session manager: %w", err))
	}
	return sm
}
