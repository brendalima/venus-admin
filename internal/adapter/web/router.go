// Package web is the hexagon's driving adapter for HTTP. It translates
// requests into calls on the catalog core and renders the results; it
// holds no business rules of its own.
package web

import (
	"log/slog"
	"net/http"

	"github.com/brendalima/venus-admin/internal/platform/config"
)

type Server struct {
	cfg    config.Config
	logger *slog.Logger
	db     Pinger
}

func NewServer(cfg config.Config, logger *slog.Logger, db Pinger) *Server {
	return &Server{cfg: cfg, logger: logger, db: db}
}

// Handler builds the routes, using net/http pattern routing (Go 1.22+),
// which already understands method and path parameters:
// "GET /admin/pecas/{id}".
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", handleLive)
	mux.Handle("GET /readyz", handleReady(s.db))

	// "/" matches every unregistered path, so it has to filter or a 404
	// would be served as the home page.
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/admin", http.StatusFound)
	})

	// Placeholder until the real screens land.
	mux.HandleFunc("GET /admin", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html>
<meta charset="utf-8">
<title>Vênus Oasis — Admin</title>
<p>Admin em construção.</p>`))
	})

	return chain(mux,
		recoverPanics(s.logger),
		logRequests(s.logger),
		secureHeaders,
	)
}
