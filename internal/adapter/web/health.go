package web

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// Pinger is the least the health check needs to know about the database,
// which keeps this adapter independent of the driver.
type Pinger interface {
	Ping(ctx context.Context) error
}

type healthResponse struct {
	Status   string `json:"status"`
	Database string `json:"database,omitempty"`
}

// handleLive answers the liveness check: is the process up?
//
// It deliberately does not touch the database. Fly uses this endpoint to
// decide whether the machine is healthy, and Neon's free tier suspends
// when idle — checking the database here would mark the machine unhealthy
// every time Postgres took a nap.
func handleLive(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, healthResponse{Status: "ok"})
}

// handleReady answers the readiness check: can we actually serve? The
// database belongs here, because without it the admin does not work.
func handleReady(db Pinger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if db == nil {
			writeJSON(w, http.StatusOK, healthResponse{
				Status:   "ok",
				Database: "not configured",
			})
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()

		if err := db.Ping(ctx); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, healthResponse{
				Status:   "unavailable",
				Database: "unreachable",
			})
			return
		}

		writeJSON(w, http.StatusOK, healthResponse{Status: "ok", Database: "ok"})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
