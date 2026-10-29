# Go Restaurant

Restaurant ordering and subscription platform using Go 1.27.3, Chi, PostgreSQL 18, Next.js 16, React 19, and Node.js 24.

## Implemented
- Group 1: backend/frontend skeleton, Docker, health/readiness checks, CI.
- Group 2: Google/Apple OAuth, opaque sessions, RBAC, audit events and security hardening.
- Group 3: restaurants, categories, meals, modifiers, prices, availability, public menus and manager-scoped admin CRUD.
- Group 4: customer profiles, owned delivery addresses, one-restaurant persisted cart, modifiers, live server prices and a cart UI.
- Group 5: checkout quotes, idempotent order snapshots, COD, Stripe-hosted payments, signed webhooks and order history.
- Group 6: weekly meal subscriptions from the cart, schedule/cutoff, auto-renewal, skips, pauses, price caps and idempotent COD delivery orders.
- Group 7: restaurant-scoped rider dispatch, secure delivery state transitions, GPS sharing, customer SSE tracking and rider management.
- Group 8: restaurant-scoped operational dashboard, AED management reporting, daily CSV export, deduplicated in-app alerts and individual read state.

## Run locally
Run: docker compose up --build

Frontend: http://localhost:3000
Catalog editor: http://localhost:3000/admin/catalog
Customer account: http://localhost:3000/account
Cart: http://localhost:3000/cart
Checkout: http://localhost:3000/checkout
Orders: http://localhost:3000/orders
Subscriptions: http://localhost:3000/subscriptions
Operations: http://localhost:3000/admin/operations
Dispatcher: http://localhost:3000/admin/deliveries
Rider deliveries: http://localhost:3000/rider/deliveries
Customer tracking: http://localhost:3000/orders/{orderID}
API health: http://localhost:8080/healthz
Database readiness: http://localhost:8080/readyz

To activate sign-in, copy .env.example to .env and supply provider credentials. See docs/auth.md for Google and Apple setup and first administrator bootstrap. See docs/catalog.md for Group 3 API, schema and manager assignments. Apple requires public HTTPS callbacks; localhost does not work for Apple sign-in.

## Test
In apps/api run go test ./... (provide DATABASE_URL to enable PostgreSQL tests). In apps/web run npm ci, npm run lint, npm run typecheck and npm run build.

CI uses Go 1.27.3 and Node.js 24. Go and npm dependencies are locked (go.sum and package-lock.json); CI verifies Go module integrity and uses npm ci.

No deployment is automatically configured.

Group 2 hardening includes database-backed OAuth start throttling, admin permission revalidation, durable session/role audit events and hourly credential pruning. See docs/auth.md.

See docs/customer-cart.md for Group 4 API, pricing and ownership rules. Checkout is intentionally deferred to Group 5.

**Group 5:** See [docs/checkout.md](docs/checkout.md). COD is enabled by Docker Compose for local development. Stripe is disabled unless both keys are configured. Tax and delivery default to zero until a merchant explicitly sets them.

See [docs/subscriptions.md](docs/subscriptions.md) for subscription scheduling, COD order generation, price ceilings, and renewal behavior.

Group 7 implementation and security requirements: [docs/deliveries.md](docs/deliveries.md). HTTPS is required for browser geolocation outside localhost.

Group 8 metric definitions and notifications: [docs/operations.md](docs/operations.md).
