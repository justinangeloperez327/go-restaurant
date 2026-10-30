package db

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrations embed.FS

func migrationChecksum(sql []byte) string {
	digest := sha256.Sum256(sql)
	return hex.EncodeToString(digest[:])
}

// Migrate applies each version atomically, verifies the SQL of already-applied
// versions, and serializes concurrent starts with an advisory transaction lock.
//
// Legacy versions from Groups 1-8 had no checksums. The first upgrade records
// their bundled checksums; subsequent starts detect any accidental modification.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(87163219)"); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "CREATE TABLE IF NOT EXISTS schema_migrations (version text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())"); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "ALTER TABLE schema_migrations ADD COLUMN IF NOT EXISTS checksum text"); err != nil {
		return err
	}

	entries, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(entries)
	for _, path := range entries {
		data, readErr := migrations.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		expected := migrationChecksum(data)
		var recorded *string
		err = tx.QueryRow(ctx, "SELECT checksum FROM schema_migrations WHERE version=$1", path).Scan(&recorded)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			if _, err = tx.Exec(ctx, string(data)); err != nil {
				return fmt.Errorf("migration %s: %w", path, err)
			}
			if _, err = tx.Exec(ctx, "INSERT INTO schema_migrations(version,checksum) VALUES($1,$2)", path, expected); err != nil {
				return err
			}
		case err != nil:
			return err
		case recorded == nil:
			// One-time migration path for previously deployed schemas.
			if _, err = tx.Exec(ctx, "UPDATE schema_migrations SET checksum=$2 WHERE version=$1 AND checksum IS NULL", path, expected); err != nil {
				return err
			}
		case *recorded != expected:
			return fmt.Errorf("migration checksum mismatch for %s: committed SQL changed after application", path)
		}
	}
	return tx.Commit(ctx)
}
