# Group 9 — Integration, security and production-readiness review

## Status
**Integration and security-hardened development build; NOT approved for production monetary transactions or live fulfillment.** See blockers below. No infrastructure was deployed and CI does not perform automatic deployment.

## Changes in Group 9
- Applied migration SQL now has SHA-256 checksums in `schema_migrations`. The first start on an existing Groups 1–8 database backfills checksums. Subsequent starts reject modified SQL. Add a **new** numbered migration for changes; never edit a previously applied version.
- `009_integrity.sql`: object/array constraints for immutable order JSON snapshots and indexes for Stripe reconciliation, dispatch queues and delivery events.
- `APP_ENV=production` rejects HTTP public API/site origins, localhost origins and the example Postgres password. Production requires at least one configured payment method. Both merchant tax and delivery charge environment variables must be explicitly set; **zero is accepted only when intentionally configured**.
- API responses receive no-sniff, no-frame and referrer headers. HTTPS requests receive HSTS (also configure HSTS on the external TLS reverse proxy). Browser pages receive security headers; browser geolocation is permitted only for the same origin.
- The Go API has request header/body read timeouts. Streaming SSE uses a 55-second bounded handler and must not be put behind a global write timeout.
- Checkout uses repeatable-read transaction isolation for a coherent cart/meal-price snapshot. Stripe Checkout URLs are returned **only when the matching provider session is persisted in Postgres**. Failed/mismatched session persistence returns a conflict rather than directing a user to an untracked payment.
- Next.js public API origin is explicitly provided at Docker **build time** for client bundles. Pages that use server-side runtime `API_PUBLIC_URL` are request-time dynamic; the public URL does not silently freeze to localhost in production.
- Manual production Compose template in `deploy/compose.production.yml` binds API/web to localhost for a separately managed HTTPS reverse proxy and uses a managed PostgreSQL connection. The local root `docker-compose.yml` is development-only.
- CI retains Go 1.27.3 race-enabled PostgreSQL tests, Go vet/format/module check, and Next.js Node 24 lint/types/build. Container build/config verification is an additional release gate.

## Production deployment — manual, not automated

1. Provision a **private managed PostgreSQL** instance (TLS required for off-host connections), unique least-privileged credentials, encrypted backups, a restore test, and a database connection limit. Never use the local `restaurant_dev` password. Migration SQL must remain unchanged after application.
2. Place HTTPS TLS termination in front of the web and API containers. Recommended same-origin routing: `https://restaurant.example.com/` → web on localhost:3000; `https://restaurant.example.com/api/v1/*`, `/healthz`, `/readyz` → API on localhost:8080. Preserve cookies and allow unbuffered delivery SSE. Restrict direct container ingress to localhost. Configure HSTS and appropriate request size limits at the reverse proxy.
3. Set all values in `deploy/production.env.example` as private environment variables (the example hostname is **not** a live service). Keep `APP_ENV=production`; it is preconfigured in the production template. Explicitly set the merchant's legal tax and delivery charge policy. `API_PUBLIC_URL` and `WEB_ORIGIN` should both be HTTPS public origins and must match the actual authentication redirect setup.
4. Verify Google/Apple OAuth callback URLs and Stripe webhook setup with real provider test credentials first. Never commit provider secrets or expose Stripe secret keys through `NEXT_PUBLIC_*`.
5. Review all blockers in the next section before accepting real payments or deliveries. When approved, from the repository root use the **manual** command:
   ```sh
   docker compose --env-file .env.production -f deploy/compose.production.yml build
   docker compose --env-file .env.production -f deploy/compose.production.yml up -d
   ```
   The production template does not include a TLS reverse proxy, DB, secrets management, cloud networking or backups. You must supply and test those separately.
6. Validate health (`/healthz`), readiness (`/readyz`), actual browser login, cart, address isolation, COD order, Stripe **test-mode** session and webhook, rider GPS permission and SSE reconnect, manager tenant isolation, subscription renewals, and operations report. Maintain evidence of every test.
7. For rollback, restore an earlier immutable container image only if the database schema remains backward-compatible. For destructive changes, perform a tested database restore to a point-in-time backup; schema version metadata is not an automatic rollback tool. Rehearse restoration in staging.

## Remaining launch blockers
- **Payment integrity:** Stripe test-mode browser E2E, timeouts, expired/missing session reconciliation, refunds/chargebacks, reconciliation of Stripe settlements, fraud/abuse controls, and controlled live-mode cutover are not completed. A failed payment consumes the original cart; retrying with the original idempotency key currently cannot always create a new session. Do not collect live money until resolved.
- **Cash on delivery:** Cash collection confirmation, reconciliation, fraud/failed delivery workflow and receipt rules have not been implemented.
- **Real deliveries:** Address geocoding, delivery-zone enforcement, service hours, inventory/capacity, rider availability, proof of delivery, cancellations and customer support workflows remain incomplete.
- **Privacy/compliance:** Retention/deletion policy for addresses, GPS and order data, marketing consent, breach procedures, access log safeguards and legally required consumer notices must be established.
- **Operations:** No dedicated worker process or job failure dead-letter queue; subscriptions and alert scans run inside the API replica. Add backups, observability, alerting, rate limits per account and abuse detection.
- **Accessibility and load testing:** Automated web lint/types/build is not a WCAG audit. Perform keyboard/screen-reader checks and realistic load/soak testing against production-equivalent infrastructure.
- **Financial reporting:** Booked value is not revenue/settled cash; VAT invoices, refunds, refunds-aware reporting and COD reconciliation are still absent.

## Security and QA verification matrix
| Area | Automated in CI | Still manual/operational |
| --- | --- | --- |
| API auth/RBAC/tenant separation | Yes: PostgreSQL integration tests | Penetration test and secrets rotation |
| Order snapshot/idempotency/payment webhook | Yes: Stripe test mock and signed event tests | Live Stripe test-mode E2E and settlement |
| Recurring subscription worker | Yes: concurrency/renewal/cutoff tests | Real scheduler alerting and downtime drills |
| Rider tracking/stream | Yes: authorization, GPS and SSE tests | HTTPS/browser permission and privacy review |
| Orders, notifications and reports | Yes: scoped SQL integration tests | Cash reconciliation and report validation |
| Database migration | Yes: repeat migration checks and checksum tests | Backup/restore and rollout rehearsal |
| Frontend | Yes: lint, typecheck, build | Accessibility, UX and device testing |
| Deployment | Docker images and Compose config checks | TLS, database, monitoring, deployment, rollback |

## Smoke test commands (development/staging)
```sh
docker compose up --build -d
curl -fsS http://localhost:8080/healthz
curl -fsS http://localhost:8080/readyz
cd apps/api && go test -race -count=1 ./...
cd ../web && npm ci && npm run lint && npm run typecheck && npm run build
```
Tests using external OAuth/Stripe require provider configuration and should be conducted with isolated test accounts and no live card charges. The default development container is not a production host.
