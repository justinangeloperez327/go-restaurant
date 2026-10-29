# Architecture decisions

## Group 1 boundaries

One Go API process, one Next.js web process, one PostgreSQL database. Start with a modular monolith. Local Docker Compose and GitHub Actions provide repeatable development and verification. No cloud account or production deployment is required.

## Modules for later groups

- Identity and sessions: Google/Apple authentication; separate server-enforced role/permission checks.
- Restaurant catalog: menus, meal availability, options, prices.
- Checkout and orders: immutable priced line items; transactional state changes and idempotency.
- Subscription schedules: subscriptions generate independent orders using a background worker.
- Delivery: rider assignment, permission-scoped GPS updates, and live status notifications.
- Admin: RBAC for every mutation and access to sensitive records.

## Real-time and jobs

Start with SSE for order-status feeds and WebSockets only where necessary for bidirectional live tracking. Introduce River (PostgreSQL job queue) when scheduled subscription work is implemented. Redis, Kubernetes, and microservices are deliberately excluded.

## Security baseline

Do not put OAuth secrets, payment keys, or production credentials in the repository. Readiness indicates database connectivity and must not disclose DSNs. The Docker Compose password is for local development only. All order/payment mutations must be authorized server-side in later groups.

## Group 1 scope exclusions

No production authentication, domain models, migrations, payment webhooks, or background worker have been implemented. They must be developed and tested with their corresponding modules rather than stubbed as though operational.
