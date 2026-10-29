package auth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

type contextKey struct{}

func (s *Service) Mount(r chi.Router) {
	r.Get("/auth/providers", s.providersList)
	r.Get("/auth/google/start", func(w http.ResponseWriter, req *http.Request) { s.start(w, req, "google") })
	r.Get("/auth/apple/start", func(w http.ResponseWriter, req *http.Request) { s.start(w, req, "apple") })
	r.Get("/auth/google/callback", func(w http.ResponseWriter, req *http.Request) { s.callback(w, req, "google") })
	r.Post("/auth/apple/callback", func(w http.ResponseWriter, req *http.Request) { s.callback(w, req, "apple") })
	r.Post("/auth/logout", s.logout)

	r.With(s.authenticated).Get("/me", s.me)
	r.Route("/admin", func(r chi.Router) {
		r.Use(s.authenticated)
		r.With(s.requirePermission("users.read")).Get("/users", s.users)
		r.With(s.requirePermission("roles.manage")).Put("/users/{userID}/roles", s.roles)
	})
}

func (s *Service) authenticated(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookie)
		if err != nil || cookie.Value == "" {
			errorResponse(w, http.StatusUnauthorized, "authentication required")
			return
		}
		user, err := s.store.SessionUser(r.Context(), cookie.Value)
		if errors.Is(err, ErrNotFound) {
			errorResponse(w, http.StatusUnauthorized, "authentication required")
			return
		}
		if err != nil {
			errorResponse(w, http.StatusServiceUnavailable, "authentication unavailable")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), contextKey{}, user)))
	})
}

func (s *Service) requirePermission(permission string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u := r.Context().Value(contextKey{}).(User)
			ok, err := s.store.HasPermission(r.Context(), u.ID, permission)
			if err != nil {
				errorResponse(w, http.StatusServiceUnavailable, "authorization unavailable")
				return
			}
			if !ok {
				errorResponse(w, http.StatusForbidden, "permission denied")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (s *Service) me(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, http.StatusOK, r.Context().Value(contextKey{}).(User))
}

func (s *Service) users(w http.ResponseWriter, r *http.Request) {
	users, err := s.store.ListUsers(r.Context())
	if err != nil {
		errorResponse(w, http.StatusServiceUnavailable, "could not list users")
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{"users": users})
}

func (s *Service) roles(w http.ResponseWriter, r *http.Request) {
	if !s.allowedOrigin(r) {
		errorResponse(w, http.StatusForbidden, "origin not allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var request struct {
		Roles []string `json:"roles"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		errorResponse(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	user := r.Context().Value(contextKey{}).(User)
	err := s.store.ReplaceRoles(r.Context(), user.ID, chi.URLParam(r, "userID"), request.Roles)
	switch {
	case errors.Is(err, ErrInvalidRoles):
		errorResponse(w, http.StatusBadRequest, "invalid roles")
	case errors.Is(err, ErrNotFound):
		errorResponse(w, http.StatusNotFound, "user not found")
	case errors.Is(err, ErrForbidden):
		errorResponse(w, http.StatusForbidden, "role change not permitted")
	case err != nil:
		errorResponse(w, http.StatusServiceUnavailable, "could not update roles")
	default:
		jsonResponse(w, http.StatusOK, map[string]any{"roles": request.Roles})
	}
}

func (s *Service) logout(w http.ResponseWriter, r *http.Request) {
	if !s.allowedOrigin(r) {
		errorResponse(w, http.StatusForbidden, "origin not allowed")
		return
	}
	if cookie, err := r.Cookie(sessionCookie); err == nil && cookie.Value != "" {
		if err := s.store.DeleteSession(r.Context(), cookie.Value); err != nil {
			errorResponse(w, http.StatusServiceUnavailable, "could not revoke session")
			return
		}
	}
	http.SetCookie(w, s.cookie(sessionCookie, "", "/", -1, false))
	w.WriteHeader(http.StatusNoContent)
}

func jsonResponse(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func errorResponse(w http.ResponseWriter, status int, message string) {
	jsonResponse(w, status, map[string]string{"error": strings.TrimSpace(message)})
}

 // Authenticated enforces a live session; catalog routes use the same middleware as admin routes.
func (s *Service) Authenticated(next http.Handler) http.Handler {
	return s.authenticated(next)
}

// UserFromContext retrieves a user already verified by Authenticated.
func UserFromContext(ctx context.Context) (User, bool) {
	u, ok := ctx.Value(contextKey{}).(User)
	return u, ok
}
