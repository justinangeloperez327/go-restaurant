package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/justinangeloperez327/go-restaurant/apps/api/internal/config"
	"github.com/justinangeloperez327/go-restaurant/apps/api/internal/db"
)

func TestVerifiedEmail(t *testing.T) {
	for _, tc := range []struct {
		value any
		want  bool
	}{
		{true, true}, {false, false}, {"true", true},
		{"TRUE", true}, {"false", false}, {nil, false},
	} {
		if got := verifiedEmail(tc.value); got != tc.want {
			t.Errorf("verifiedEmail(%v) = %t, want %t", tc.value, got, tc.want)
		}
	}
}

func TestCookieAndOrigins(t *testing.T) {
	s := &Service{cfg: config.Config{APIBaseURL: "https://api.example.test", WebOrigin: "https://example.test", SecureCookies: true}}
	c := s.cookie("test", "secret", "/", 600, true)
	if !c.Secure || c.SameSite != http.SameSiteNoneMode || !c.HttpOnly {
		t.Fatalf("Apple callback cookie must be Secure, SameSite=None, HttpOnly: %+v", c)
	}
	r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	r.Header.Set("Origin", "https://example.test")
	if !s.allowedOrigin(r) {
		t.Fatal("expected allowed frontend origin")
	}
	r.Header.Set("Origin", "https://attacker.test")
	if s.allowedOrigin(r) {
		t.Fatal("cross-site origin accepted")
	}
}

func TestNoProviderConfigured(t *testing.T) {
	s, err := NewService(config.Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	r.Route("/api/v1", s.Mount)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/auth/google/start", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/auth/providers", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "[]") {
		t.Fatalf("providers: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/me", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated /me: %d", w.Code)
	}
}

func TestPersistenceAndRBAC(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL required")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Close() })
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	store := Store{DB: pool}
	subject, err := randomString()
	if err != nil {
		t.Fatal(err)
	}
	id, err := store.ResolveIdentity(ctx, "google", subject, "verified@example.test", "Tester")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id=$1::uuid", id) })
	id2, err := store.ResolveIdentity(ctx, "google", subject, "verified@example.test", "Tester")
	if err != nil || id != id2 {
		t.Fatalf("identity was not stable: %v %q %q", err, id, id2)
	}
	// Same email from a different provider is a different user: no automatic linking.
	other, err := store.ResolveIdentity(ctx, "apple", subject, "verified@example.test", "Tester")
	if err != nil || other == id {
		t.Fatalf("provider identities incorrectly linked: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id=$1::uuid", other) })
	rawToken, err := randomString()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.NewSession(ctx, id, rawToken); err != nil {
		t.Fatal(err)
	}
	user, err := store.SessionUser(ctx, rawToken)
	if err != nil || user.ID != id || len(user.Roles) != 1 || user.Roles[0] != "customer" {
		t.Fatalf("unexpected user: %+v, %v", user, err)
	}
	allowed, err := store.HasPermission(ctx, id, "roles.manage")
	if err != nil || allowed {
		t.Fatalf("customer received admin permissions: %t %v", allowed, err)
	}
	if !errors.Is(store.ReplaceRoles(ctx, id, id, []string{"admin", "admin"}), ErrInvalidRoles) {
		t.Fatal("duplicate role should be rejected")
	}
	if _, err := pool.Exec(ctx, "INSERT INTO user_roles (user_id,role_name) VALUES ($1::uuid,'admin')", id); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceRoles(ctx, id, other, []string{"rider", "customer"}); err != nil {
		t.Fatal(err)
	}
	allowed, err = store.HasPermission(ctx, other, "delivery.manage")
	if err != nil || !allowed {
		t.Fatalf("rider has no delivery permission: %t %v", allowed, err)
	}
	if !errors.Is(store.ReplaceRoles(ctx, id, id, []string{"customer"}), ErrForbidden) {
		t.Fatal("self-demotion should be blocked")
	}
	if err := store.DeleteSession(ctx, rawToken); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SessionUser(ctx, rawToken); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoked session still valid: %v", err)
	}
	state, err := randomString()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.BeginFlow(ctx, state, "google", "pkce", "nonce"); err != nil {
		t.Fatal(err)
	}
	flow, err := store.ConsumeFlow(ctx, state, "google")
	if err != nil || flow.Verifier != "pkce" || flow.Nonce != "nonce" {
		t.Fatalf("flow not consumed: %+v %v", flow, err)
	}
	if _, err := store.ConsumeFlow(ctx, state, "google"); !errors.Is(err, ErrNotFound) {
		t.Fatal("flow state replay accepted")
	}
}
