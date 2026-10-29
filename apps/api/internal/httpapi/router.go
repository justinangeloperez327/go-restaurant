package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type databaseCheck func(context.Context) error

type statusResponse struct {
	Status   string `json:"status"`
	Database string `json:"database,omitempty"`
}

func NewRouter(check databaseCheck) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, statusResponse{Status: "ok"})
	})

	r.Get("/readyz", func(w http.ResponseWriter, req *http.Request) {
		ctx, cancel := context.WithTimeout(req.Context(), 2*time.Second)
		defer cancel()
		if check == nil || check(ctx) != nil {
			writeJSON(w, http.StatusServiceUnavailable, statusResponse{
				Status: "unavailable", Database: "down",
			})
			return
		}
		writeJSON(w, http.StatusOK, statusResponse{Status: "ready", Database: "up"})
	})

	return r
}

func writeJSON(w http.ResponseWriter, code int, value statusResponse) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(value)
}
