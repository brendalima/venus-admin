package rest

import (
	"log/slog"
	"net/http"
)

// Admin serves the admin screens. Placeholder until the real ones land.
type Admin struct{}

func (a *Admin) Home(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/admin", http.StatusFound)
}

func (a *Admin) Index(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if _, err := w.Write([]byte(`<!doctype html>
<meta charset="utf-8">
<title>Vênus Oasis — Admin</title>
<p>Admin em construção.</p>`)); err != nil {
		slog.Error("couldn't write response", "error", err)
	}
}
