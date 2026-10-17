package rest

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type mockHealthService struct{ err error }

func (m mockHealthService) Ready(context.Context) error { return m.err }

func TestNewHealth(t *testing.T) {
	if _, err := NewHealth(nil); err == nil {
		t.Error("a nil service should be rejected")
	}
}

func TestLivenessIgnoresDatabase(t *testing.T) {
	// Liveness must not depend on the database: Neon's free tier sleeps,
	// and that does not make the machine unhealthy.
	h := MustNewHealth(mockHealthService{err: errors.New("database is down")})

	rec := httptest.NewRecorder()
	h.Live(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestReadiness(t *testing.T) {
	tests := []struct {
		name       string
		serviceErr error
		wantStatus int
		wantBody   map[string]string
	}{
		{"database answering", nil, http.StatusOK, map[string]string{"status": "ok", "database": "ok"}},
		{"database down", errors.New("timeout"), http.StatusServiceUnavailable, map[string]string{"error": "database unreachable"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := MustNewHealth(mockHealthService{err: tt.serviceErr})

			rec := httptest.NewRecorder()
			h.Ready(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}

			var got map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("response is not valid JSON: %v", err)
			}
			for k, want := range tt.wantBody {
				if got[k] != want {
					t.Errorf("%s = %q, want %q", k, got[k], want)
				}
			}
		})
	}
}
