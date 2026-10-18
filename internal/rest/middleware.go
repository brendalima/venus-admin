package rest

import (
	"log/slog"
	"net/http"
	"time"
)

// statusRecorder captures the status code for the access log, since
// http.ResponseWriter does not expose what was written.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

// Unwrap lets http.ResponseController reach the connection underneath,
// which the photo upload needs to extend its deadlines.
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

// LogRequests writes one structured line per request.
func LogRequests(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w}

			next.ServeHTTP(rec, r)

			if rec.status == 0 {
				rec.status = http.StatusOK
			}
			logger.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"bytes", rec.bytes,
				"duration", time.Since(start).Round(time.Microsecond).String(),
			)
		})
	}
}

// RecoverPanics keeps a panic in one handler from taking down the whole
// process, and every other in-flight request with it.
func RecoverPanics(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					logger.Error("panic in handler",
						"error", err,
						"method", r.Method,
						"path", r.URL.Path,
					)
					// User-facing text stays in Portuguese: the people
					// running this admin are Brazilian.
					http.Error(w, "Erro interno", http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// SecureHeaders applies baseline security headers.
//
// The CSP is restrictive from the start: the admin is server-rendered
// HTML and needs no external script beyond HTMX, which is served from
// /static. blob: images are the previews of photos chosen but not yet
// sent (static/photos.js).
func SecureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Content-Security-Policy",
			"default-src 'self'; img-src 'self' data: blob: https:; "+
				"style-src 'self' 'unsafe-inline'; script-src 'self'; "+
				"frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		next.ServeHTTP(w, r)
	})
}

// RejectCrossOrigin blocks state-changing requests sent by another site
// (CSRF), using the browser's Sec-Fetch-Site and Origin headers. Together
// with the SameSite=Lax session cookie this replaces CSRF tokens, which
// would mean a hidden field in every form.
func RejectCrossOrigin(next http.Handler) http.Handler {
	cop := http.NewCrossOriginProtection()
	cop.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slog.WarnContext(r.Context(), "cross-origin request rejected",
			"method", r.Method, "path", r.URL.Path, "origin", r.Header.Get("Origin"))
		http.Error(w, "Pedido recusado: ele veio de outro site.", http.StatusForbidden)
	}))
	return cop.Handler(next)
}

// Chain applies middleware in the order given: the first is outermost,
// meaning the first to see the request.
func Chain(h http.Handler, mw ...func(http.Handler) http.Handler) http.Handler {
	for i := len(mw) - 1; i >= 0; i-- {
		h = mw[i](h)
	}
	return h
}
