# Group 2: Google/Apple authentication and RBAC

## Google setup
Create OAuth Web application credentials in Google Cloud Console and register redirect URI http://localhost:8080/api/v1/auth/google/callback. Copy .env.example to .env, set GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET, and run docker compose up --build. Open http://localhost:3000 and sign in.

## Apple setup
Apple web Sign in requires a public HTTPS callback URL (Apple does not allow localhost). Register a Services ID, domain, and return URL https://YOUR_API_HOST/api/v1/auth/apple/callback. Configure APPLE_CLIENT_ID, APPLE_TEAM_ID, APPLE_KEY_ID, APPLE_PRIVATE_KEY_PEM and HTTPS API_PUBLIC_URL. Apple's cross-site POST callback requires a Secure SameSite=None OAuth state cookie. The Go backend generates an ES256 Apple client-secret JWT.

Providers are disabled until credentials are provided. No live credentials are bundled.

## Security
Google uses authorization-code OAuth with PKCE S256. Apple uses authorization-code OAuth with state and nonce. Both verify OIDC signature, issuer, audience, expiry and nonce. OAuth state is single-use, persisted by SHA-256 digest, and also checked against a browser cookie. Sessions are random, opaque, HttpOnly cookies; the database holds SHA-256 token digests. Sessions expire after seven days and can be revoked. New users receive only customer role; matching emails from two identity providers are never automatically linked.

## API endpoints

| Method | Path | Authorization |
|---|---|---|
| GET | /api/v1/auth/providers | Public |
| GET | /api/v1/auth/google/start | Configured provider |
| GET | /api/v1/auth/google/callback | OAuth state + OIDC validation |
| GET | /api/v1/auth/apple/start | Configured provider |
| POST | /api/v1/auth/apple/callback | OAuth state + OIDC validation |
| GET | /api/v1/me | Session |
| POST | /api/v1/auth/logout | Allowed Origin |
| GET | /api/v1/admin/users | users.read |
| PUT | /api/v1/admin/users/{userID}/roles | roles.manage + allowed Origin |

## First administrator
After signing in, obtain your UUID from /api/v1/me. An authorized database operator can grant the first admin role with:

    INSERT INTO user_roles (user_id, role_name) VALUES ('YOUR-USER-UUID'::uuid, 'admin') ON CONFLICT DO NOTHING;

Do not provide a public admin signup endpoint. Role mutation blocks admin self-demotion and removal of the last active administrator.

## Before production
Use trusted HTTPS ingress and compatible same-site web/API origins. Add rate limiting, audit trails, real-provider integration tests, session cleanup scheduling, admin pagination, and committed npm and Go dependency lock metadata. Startup applies embedded migrations transactionally. Do not rewrite previously applied migration files; add numbered versions. 


## Group 2 hardening (continued)

- A database-backed, atomic per-provider and per-server-observed-IP limiter permits 20 OAuth starts per rolling 10-minute fixed window; subsequent starts return 429 with Retry-After. In deployments behind a reverse proxy this limits the **proxy's** address unless a trusted client-IP forwarding mechanism is explicitly configured. Never blindly trust X-Forwarded-For from the public internet.
- Role assignments recheck the acting administrator's current permission inside a serialized transaction, preventing an authorization-check/revocation race. The final-active-admin safeguard remains in place.
- Persistent audit events capture successful session issuance, session revocation, and role mutations. They store user IDs and new role names, but not bearer tokens, identity tokens, OAuth codes, or raw IP addresses.
- Expired OAuth states, sessions, and rate-limit counters are pruned hourly by the Go API, with a bounded database timeout. Multiple replicas can prune concurrently.
- JSON role mutation payloads now reject trailing second documents.
- Real Google/Apple provider login still requires credentials and a browser-backed end-to-end test; the CI suite tests local security and persistence behavior, not live identity providers.

Audit events currently have no automatic retention policy; define retention and access controls before production deployment.
