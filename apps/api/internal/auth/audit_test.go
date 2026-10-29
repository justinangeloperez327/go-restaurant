package auth

import (
	"context"
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

func TestRoleMutationAndAudit(t *testing.T) {
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
	subject, _ := randomString()
	adminID, err := store.ResolveIdentity(ctx, "google", subject, "", "admin")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id=$1::uuid", adminID) })
	otherSubject, _ := randomString()
	targetID, err := store.ResolveIdentity(ctx, "apple", otherSubject, "", "target")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id=$1::uuid", targetID) })
	if _, err := pool.Exec(ctx, "INSERT INTO user_roles (user_id, role_name) VALUES ($1::uuid, 'admin')", adminID); err != nil {
		t.Fatal(err)
	}
	token, _ := randomString()
	if err := store.NewSession(ctx, adminID, token); err != nil {
		t.Fatal(err)
	}
	service := &Service{store: store, cfg: config.Config{WebOrigin: "http://localhost:3000"}}
	router := chi.NewRouter()
	router.Route("/api/v1", service.Mount)
	route := "/api/v1/admin/users/" + targetID + "/roles"
	send := func(origin, body string) int {
		t.Helper()
		req := httptest.NewRequest(http.MethodPut, route, strings.NewReader(body))
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec.Code
	}
	if got := send("", `{"roles":["rider"]}`); got != http.StatusForbidden {
		t.Fatalf("missing Origin: got %d", got)
	}
	if got := send("https://attacker.example", `{"roles":["rider"]}`); got != http.StatusForbidden {
		t.Fatalf("cross-site Origin: got %d", got)
	}
	if got := send("http://localhost:3000", `{"roles":["rider"]} {"roles":["admin"]}`); got != http.StatusBadRequest {
		t.Fatalf("trailing JSON was accepted: %d", got)
	}
	if got := send("http://localhost:3000", `{"roles":["rider","customer"]}`); got != http.StatusOK {
		t.Fatalf("admin role mutation failed: %d", got)
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM auth_audit_events WHERE event_type='roles.updated' AND actor_user_id=$1::uuid AND target_user_id=$2::uuid", adminID, targetID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("role change audit not created: count=%d err=%v", count, err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM auth_audit_events WHERE event_type='login.succeeded' AND actor_user_id=$1::uuid", adminID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("session creation audit not created: count=%d err=%v", count, err)
	}
	if err := store.DeleteSession(ctx, token); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM auth_audit_events WHERE event_type='session.revoked' AND actor_user_id=$1::uuid", adminID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("session revocation audit not created: count=%d err=%v", count, err)
	}
}
