package db

import (
	"context"
	"encoding/hex"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestChecksumDeterminismAndEditDetection(t *testing.T) {
	a := migrationChecksum([]byte("CREATE TABLE test(id INT);\n"))
	b := migrationChecksum([]byte("CREATE TABLE test(id INT);\n"))
	modified := migrationChecksum([]byte("CREATE TABLE test(id BIGINT);\n"))
	if a != b || a == modified || len(a) != 64 {
		t.Fatalf("unexpected migration checksum behavior")
	}
	if _, err := hex.DecodeString(a); err != nil {
		t.Fatalf("invalid checksum: %v", err)
	}
}
func TestExistingMigrationChecksumsArePersisted(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("PostgreSQL integration requires DATABASE_URL")
	}
	db, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query(context.Background(), "SELECT version,checksum FROM schema_migrations ORDER BY version")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	found := 0
	for rows.Next() {
		var version, checksum string
		if err := rows.Scan(&version, &checksum); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(version, "migrations/") || len(checksum) != 64 {
			t.Fatalf("invalid migration metadata %s", version)
		}
		found++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if found < 8 {
		t.Fatalf("expected at least eight applied schema versions, got %d", found)
	}
}
