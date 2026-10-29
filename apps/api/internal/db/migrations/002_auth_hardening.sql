-- Group 2 hardening: rate limiting and durable, non-sensitive audit events.
CREATE TABLE IF NOT EXISTS auth_rate_limits (
  key_hash bytea PRIMARY KEY,
  attempts integer NOT NULL CHECK (attempts > 0),
  reset_at timestamptz NOT NULL
);
CREATE INDEX IF NOT EXISTS auth_rate_limits_expiry_idx ON auth_rate_limits(reset_at);

CREATE TABLE IF NOT EXISTS auth_audit_events (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  event_type text NOT NULL CHECK (event_type IN ('login.succeeded', 'session.revoked', 'roles.updated')),
  actor_user_id uuid REFERENCES users(id) ON DELETE SET NULL,
  target_user_id uuid REFERENCES users(id) ON DELETE SET NULL,
  details jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS auth_audit_events_actor_idx ON auth_audit_events (actor_user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS auth_audit_events_created_idx ON auth_audit_events (created_at DESC);
