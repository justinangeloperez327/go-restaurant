# Go Restaurant

Restaurant ordering and subscription platform using Go 1.27.3, Chi, PostgreSQL 18, Next.js 16, React 19, and Node.js 24.

## Implemented
- Group 1: backend/frontend skeleton, Docker, health/readiness checks, CI.
- Group 2: Google/Apple OAuth endpoints, OIDC token verification, user identities, persistent sessions, role/permission tables, protected admin user listing, migrations, tests and web account panel.

## Run locally
Run: docker compose up --build

Frontend: http://localhost:3000
API health: http://localhost:8080/healthz
Database readiness: http://localhost:8080/readyz

To activate sign-in, copy .env.example to .env and supply provider credentials. See docs/auth.md for Google and Apple setup and first administrator bootstrap. Apple requires public HTTPS callbacks; localhost does not work for Apple sign-in.

## Test
In apps/api run go test ./... (provide DATABASE_URL to enable PostgreSQL tests). In apps/web run npm ci, npm run lint, npm run typecheck and npm run build.

CI uses Go 1.27.3 and Node.js 24. Go and npm dependencies are locked (go.sum and package-lock.json); CI verifies Go module integrity and uses npm ci.

No deployment is automatically configured.

Group 2 hardening includes database-backed OAuth start throttling, admin permission revalidation, durable session/role audit events and hourly credential pruning. See docs/auth.md.
