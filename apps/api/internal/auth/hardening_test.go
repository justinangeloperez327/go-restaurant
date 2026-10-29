package auth

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/justinangeloperez327/go-restaurant/apps/api/internal/db"
)

func TestLoginStartRateLimit(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL required for rate-limit integration test")
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
	s := Store{DB: pool}
	identifier, err := randomString()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		ok, err := s.AllowLoginStart(ctx, "google", identifier)
		if err != nil || !ok {
			t.Fatalf("attempt %d: allowed=%v err=%v", i+1, ok, err)
		}
	}
	ok, err := s.AllowLoginStart(ctx, "google", identifier)
	if err != nil || ok {
		t.Fatalf("21st attempt must be rejected: allowed=%v err=%v", ok, err)
	}
	ok, err = s.AllowLoginStart(ctx, "apple", identifier)
	if err != nil || !ok {
		t.Fatalf("provider-specific limit was shared: allowed=%v err=%v", ok, err)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM auth_rate_limits WHERE key_hash=$1", digest("oauth-start:google:"+identifier)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM auth_rate_limits WHERE key_hash=$1", digest("oauth-start:apple:"+identifier)); err != nil {
		t.Fatal(err)
	}
}

func TestNoPrivilegeEscalationViaStore(t *testing.T) {
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
	s := Store{DB: pool}
	subject, err := randomString()
	if err != nil {
		t.Fatal(err)
	}
	customer, err := s.ResolveIdentity(ctx, "google", subject, "", "customer")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id=$1::uuid", customer) })
	subject2, _ := randomString()
	other, err := s.ResolveIdentity(ctx, "apple", subject2, "", "other")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id=$1::uuid", other) })
	if err := s.ReplaceRoles(ctx, customer, other, []string{"admin"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("non-admin directly granted administrator: %v", err)
	}
}
