package rest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/brendalima/venus-admin/internal/core/services"
)

type healthResponse struct {
	Status   string `json:"status"`
	Database string `json:"database,omitempty"`
}

type Health struct {
	service services.HealthService
}

func NewHealth(service services.HealthService) (*Health, error) {
	if service == nil {
		return nil, errors.New("missing health service dependency")
	}
	return &Health{service: service}, nil
}

func MustNewHealth(service services.HealthService) *Health {
	h, err := NewHealth(service)
	if err != nil {
		panic(fmt.Errorf("couldn't create health handler: %w", err))
	}
	return h
}

// Live answers the liveness check: is the process up?
//
// It deliberately does not touch the database. Fly uses this endpoint to
// decide whether the machine is healthy, and Neon's free tier suspends
// when idle — checking the database here would mark the machine unhealthy
// every time Postgres took a nap.
func (h *Health) Live(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, healthResponse{Status: "ok"})
}

// Ready answers the readiness check: can we actually serve? The database
// belongs here, because without it the admin does not work.
func (h *Health) Ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	if err := h.service.Ready(ctx); err != nil {
		slog.Warn("readiness check failed", "error", err)
		writeError(w, http.StatusServiceUnavailable, "database unreachable")
		return
	}
	writeJSON(w, http.StatusOK, healthResponse{Status: "ok", Database: "ok"})
}
