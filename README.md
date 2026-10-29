# Go Restaurant

An online restaurant meal ordering and subscription platform. Group 1 establishes the development foundation; authentication, menus, orders, subscriptions, riders, and RBAC are delivered in subsequent groups.

## Stack

- Go 1.27.3, Chi, pgx/pgxpool, PostgreSQL 18
- Next.js 16.4.3, React 19.3.1, TypeScript, Node.js 24
- Docker Compose for local development, GitHub Actions for CI

## Quick start (Docker)

Install Docker Desktop, then run:

```bash
docker compose up --build
```

Open **http://localhost:3000** (web) and **http://localhost:8080/healthz** (API).

- `GET /healthz`: liveness; returns 200 when the HTTP process responds.
- `GET /readyz`: readiness; returns 200 only if PostgreSQL can be pinged, otherwise 503.
- The web homepage reads the API health endpoint server-side.
- Local PostgreSQL listens on `127.0.0.1:5432`; credentials are development-only.

Stop with `docker compose down`. To delete all **local** database data, run `docker compose down -v`.

## Run without Docker (applications)

Start PostgreSQL using `docker compose up -d db`, then run the API:

```bash
cd apps/api
# bash:
export DATABASE_URL='postgres://restaurant:restaurant_dev@localhost:5432/restaurant?sslmode=disable'
# PowerShell: $env:DATABASE_URL = 'postgres://restaurant:restaurant_dev@localhost:5432/restaurant?sslmode=disable'
go run ./cmd/server
```

In another terminal:

```bash
cd apps/web
npm install
npm run dev
```

The Next.js app uses `API_INTERNAL_URL` (default `http://localhost:8080`) for server-side health checks. Do not expose the internal Docker hostname to browsers.

## Verify

```bash
cd apps/api
go test ./...
go vet ./...
go build ./cmd/server
```

```bash
cd apps/web
npm install
npm run lint
npm run typecheck
npm run build
```

The API includes readiness tests and an optional real-PostgreSQL integration test (enabled when `DATABASE_URL` is configured). CI runs this with a PostgreSQL service.

## Scope and next steps

Group 1 intentionally contains **no mock authentication, ordering, payment processing, or rider-tracking implementations**. Schema migrations, sqlc query generation, Google/Apple identity, and RBAC will be introduced with their actual business modules. See `docs/architecture.md` and `docs/openapi.yaml`.

**Dependency lockfiles:** The initial scaffold uses exact direct npm dependency versions and resolves transitive dependencies at install time. Generate and commit `apps/web/package-lock.json` before production/release workflows; until then CI uses `npm install` rather than pretending a lockfile exists. Likewise, `go mod tidy` during CI resolves and checksums the Go module graph; its resulting `go.sum` should be committed after the first successful dependency resolution.

No deployment is configured by this commit.
