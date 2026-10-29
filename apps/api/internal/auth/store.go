package auth

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct{ DB *pgxpool.Pool }

type User struct {
	ID          string   `json:"id"`
	Email       string   `json:"email"`
	DisplayName string   `json:"display_name"`
	Roles       []string `json:"roles"`
}

type Flow struct {
	Verifier string
	Nonce    string
}

var ErrNotFound = errors.New("not found")
var ErrForbidden = errors.New("forbidden")
var ErrInvalidRoles = errors.New("invalid roles")
var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func digest(raw string) []byte {
	sum := sha256.Sum256([]byte(raw))
	return sum[:]
}

func (s Store) BeginFlow(ctx context.Context, rawState, provider, verifier, nonce string) error {
	_, err := s.DB.Exec(ctx, `INSERT INTO oauth_flows (state_hash, provider, verifier, nonce, expires_at)
		VALUES ($1,$2,$3,$4,now() + interval '10 minutes')`,
		digest(rawState), provider, verifier, nonce)
	return err
}

func (s Store) ConsumeFlow(ctx context.Context, rawState, provider string) (Flow, error) {
	var f Flow
	err := s.DB.QueryRow(ctx, `DELETE FROM oauth_flows
		WHERE state_hash=$1 AND provider=$2 AND expires_at > now()
		RETURNING verifier, nonce`, digest(rawState), provider).Scan(&f.Verifier, &f.Nonce)
	if errors.Is(err, pgx.ErrNoRows) {
		return f, ErrNotFound
	}
	return f, err
}

// ResolveIdentity never links accounts by email; only the provider's stable verified subject is trusted.
func (s Store) ResolveIdentity(ctx context.Context, provider, subject, email, name string) (string, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var id string
	if err := tx.QueryRow(ctx, `INSERT INTO users (email, display_name)
		VALUES (NULLIF($1,''), $2) RETURNING id::text`, email, name).Scan(&id); err != nil {
		return "", err
	}
	var linked string
	err = tx.QueryRow(ctx, `INSERT INTO identities (provider,subject,user_id,verified_email)
		VALUES ($1,$2,$3::uuid,NULLIF($4,'')) ON CONFLICT (provider,subject) DO NOTHING
		RETURNING user_id::text`, provider, subject, id, email).Scan(&linked)
	if errors.Is(err, pgx.ErrNoRows) {
		// A concurrently or previously established identity won the unique constraint.
		_ = tx.Rollback(ctx)
		err = s.DB.QueryRow(ctx, `SELECT i.user_id::text FROM identities i
			JOIN users u ON u.id=i.user_id
			WHERE i.provider=$1 AND i.subject=$2 AND u.is_active`, provider, subject).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrForbidden
		}
		return id, err
	}
	if err != nil {
		return "", err
	}
	if _, err = tx.Exec(ctx, "INSERT INTO user_roles (user_id, role_name) VALUES ($1::uuid,'customer')", linked); err != nil {
		return "", err
	}
	if err = tx.Commit(ctx); err != nil {
		return "", err
	}
	return linked, nil
}

func (s Store) NewSession(ctx context.Context, userID, rawToken string) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `INSERT INTO sessions (token_hash,user_id,expires_at)
		VALUES ($1,$2::uuid,now() + interval '7 days')`, digest(rawToken), userID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO auth_audit_events (event_type, actor_user_id)
		VALUES ('login.succeeded', $1::uuid)`, userID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s Store) DeleteSession(ctx context.Context, token string) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var userID string
	err = tx.QueryRow(ctx, `DELETE FROM sessions WHERE token_hash=$1
		RETURNING user_id::text`, digest(token)).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO auth_audit_events (event_type, actor_user_id)
		VALUES ('session.revoked', $1::uuid)`, userID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s Store) SessionUser(ctx context.Context, token string) (User, error) {
	var u User
	err := s.DB.QueryRow(ctx, `SELECT u.id::text, COALESCE(u.email,''), u.display_name
		FROM sessions s JOIN users u ON u.id=s.user_id
		WHERE s.token_hash=$1 AND s.expires_at>now() AND u.is_active`,
		digest(token)).Scan(&u.ID, &u.Email, &u.DisplayName)
	if errors.Is(err, pgx.ErrNoRows) {
		return u, ErrNotFound
	}
	if err != nil {
		return u, err
	}
	rows, err := s.DB.Query(ctx, "SELECT role_name FROM user_roles WHERE user_id=$1::uuid ORDER BY role_name", u.ID)
	if err != nil {
		return u, err
	}
	defer rows.Close()
	u.Roles = []string{}
	for rows.Next() {
		var role string
		if err := rows.Scan(&role); err != nil {
			return u, err
		}
		u.Roles = append(u.Roles, role)
	}
	return u, rows.Err()
}

func (s Store) HasPermission(ctx context.Context, userID, permission string) (bool, error) {
	var ok bool
	err := s.DB.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM user_roles ur JOIN role_permissions rp ON rp.role_name=ur.role_name
		WHERE ur.user_id=$1::uuid AND rp.permission_name=$2)`, userID, permission).Scan(&ok)
	return ok, err
}

func (s Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.DB.Query(ctx, `SELECT u.id::text,COALESCE(u.email,''),u.display_name,
		COALESCE(array_agg(ur.role_name ORDER BY ur.role_name)
		FILTER (WHERE ur.role_name IS NOT NULL),ARRAY[]::text[])
		FROM users u LEFT JOIN user_roles ur ON ur.user_id=u.id
		GROUP BY u.id ORDER BY u.created_at DESC LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := []User{}
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Email, &u.DisplayName, &u.Roles); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (s Store) ReplaceRoles(ctx context.Context, actorID, targetID string, roles []string) error {
	if !uuidPattern.MatchString(targetID) || len(roles) == 0 || len(roles) > 4 {
		return ErrInvalidRoles
	}
	seen := map[string]bool{}
	valid := map[string]bool{"admin": true, "customer": true, "rider": true, "restaurant_manager": true}
	for _, role := range roles {
		if !valid[role] || seen[role] {
			return ErrInvalidRoles
		}
		seen[role] = true
	}
	if actorID == targetID && !seen["admin"] {
		return ErrForbidden // Never allow an admin to remove their own administrator role.
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(87163220)"); err != nil {
		return err
	}
	// Revalidate the actor inside the same serialized transaction. A previous
	// authorization middleware check may have raced with administrator revocation.
	var authorized bool
	err = tx.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM users u
		JOIN user_roles ur ON ur.user_id=u.id
		JOIN role_permissions rp ON rp.role_name=ur.role_name
		WHERE u.id=$1::uuid AND u.is_active AND rp.permission_name='roles.manage'
	)`, actorID).Scan(&authorized)
	if err != nil {
		return err
	}
	if !authorized {
		return ErrForbidden
	}
	var active bool
	err = tx.QueryRow(ctx, "SELECT is_active FROM users WHERE id=$1::uuid FOR UPDATE", targetID).Scan(&active)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if !active {
		return ErrForbidden
	}
	if _, err = tx.Exec(ctx, "DELETE FROM user_roles WHERE user_id=$1::uuid", targetID); err != nil {
		return err
	}
	for _, role := range roles {
		if _, err = tx.Exec(ctx, "INSERT INTO user_roles(user_id,role_name) VALUES ($1::uuid,$2)", targetID, role); err != nil {
			return fmt.Errorf("insert role: %w", err)
		}
	}
	// Serialize changes and ensure at least one active administrator survives.
	var count int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM user_roles ur JOIN users u ON u.id=ur.user_id
		WHERE ur.role_name='admin' AND u.is_active`).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		return ErrForbidden
	}
	details, err := json.Marshal(map[string]any{"roles": roles})
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO auth_audit_events
		(event_type, actor_user_id, target_user_id, details)
		VALUES ('roles.updated', $1::uuid, $2::uuid, $3::jsonb)`,
		actorID, targetID, details); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Prune removes expired credentials; safe to call periodically from a scheduled worker.
func (s Store) Prune(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, err := s.DB.Exec(ctx, "DELETE FROM oauth_flows WHERE expires_at < now()"); err != nil {
		return err
	}
	if _, err := s.DB.Exec(ctx, "DELETE FROM sessions WHERE expires_at < now()"); err != nil {
		return err
	}
	_, err := s.DB.Exec(ctx, "DELETE FROM auth_rate_limits WHERE reset_at < now()")
	return err
}
