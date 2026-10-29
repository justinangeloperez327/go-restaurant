CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS users (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  email text,
  display_name text NOT NULL DEFAULT '',
  is_active boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS identities (
  provider text NOT NULL CHECK (provider IN ('google', 'apple')),
  subject text NOT NULL,
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  verified_email text,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (provider, subject),
  UNIQUE (provider, user_id)
);

CREATE TABLE IF NOT EXISTS oauth_flows (
  state_hash bytea PRIMARY KEY,
  provider text NOT NULL CHECK (provider IN ('google', 'apple')),
  verifier text NOT NULL,
  nonce text NOT NULL,
  expires_at timestamptz NOT NULL
);
CREATE INDEX IF NOT EXISTS oauth_flows_expiry_idx ON oauth_flows (expires_at);

CREATE TABLE IF NOT EXISTS sessions (
  token_hash bytea PRIMARY KEY,
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS sessions_user_id_idx ON sessions (user_id);
CREATE INDEX IF NOT EXISTS sessions_expiry_idx ON sessions (expires_at);

CREATE TABLE IF NOT EXISTS roles (
  name text PRIMARY KEY
);
CREATE TABLE IF NOT EXISTS permissions (
  name text PRIMARY KEY
);
CREATE TABLE IF NOT EXISTS role_permissions (
  role_name text NOT NULL REFERENCES roles(name) ON DELETE CASCADE,
  permission_name text NOT NULL REFERENCES permissions(name) ON DELETE CASCADE,
  PRIMARY KEY (role_name, permission_name)
);
CREATE TABLE IF NOT EXISTS user_roles (
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  role_name text NOT NULL REFERENCES roles(name) ON DELETE CASCADE,
  PRIMARY KEY (user_id, role_name)
);
CREATE INDEX IF NOT EXISTS user_roles_role_idx ON user_roles (role_name);

INSERT INTO roles (name) VALUES
('customer'), ('rider'), ('restaurant_manager'), ('admin')
ON CONFLICT DO NOTHING;

INSERT INTO permissions (name) VALUES
('users.read'), ('roles.manage'), ('catalog.manage'), ('orders.manage'),
('subscriptions.manage'), ('delivery.manage')
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (role_name, permission_name)
SELECT 'admin', name FROM permissions ON CONFLICT DO NOTHING;
INSERT INTO role_permissions (role_name, permission_name) VALUES
('restaurant_manager','catalog.manage'),
('restaurant_manager','orders.manage'),
('restaurant_manager','subscriptions.manage'),
('rider','delivery.manage')
ON CONFLICT DO NOTHING;
