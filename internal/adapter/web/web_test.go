package web

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/brendalima/venus-admin/internal/platform/config"
)

type stubPinger struct{ err error }

func (s stubPinger) Ping(context.Context) error { return s.err }

func newTestServer(db Pinger) http.Handler {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewServer(config.Config{Env: config.EnvDev, Port: 8080}, logger, db).Handler()
}

func TestLivenessIgnoresDatabase(t *testing.T) {
	// Liveness must not depend on the database: Neon's free tier sleeps,
	// and that does not make the machine unhealthy.
	h := newTestServer(stubPinger{err: errors.New("database is down")})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestReadiness(t *testing.T) {
	tests := []struct {
		name       string
		db         Pinger
		wantStatus int
		wantDB     string
	}{
		{"no database configured", nil, http.StatusOK, "not configured"},
		{"database answering", stubPinger{}, http.StatusOK, "ok"},
		{"database down", stubPinger{err: errors.New("timeout")}, http.StatusServiceUnavailable, "unreachable"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			newTestServer(tt.db).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}

			var got healthResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("response is not valid JSON: %v", err)
			}
			if got.Database != tt.wantDB {
				t.Errorf("database = %q, want %q", got.Database, tt.wantDB)
			}
		})
	}
}

func TestUnknownPathIs404(t *testing.T) {
	// "/" matches everything in ServeMux, so an unknown path could fall
	// through to the home page instead of returning 404.
	rec := httptest.NewRecorder()
	newTestServer(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/does-not-exist", nil))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestSecurityHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	newTestServer(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	for _, h := range []string{"X-Content-Type-Options", "X-Frame-Options", "Content-Security-Policy"} {
		if rec.Header().Get(h) == "" {
			t.Errorf("missing header %s", h)
		}
	}
}

func TestPanicIsContained(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := chain(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }),
		recoverPanics(logger),
	)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}
