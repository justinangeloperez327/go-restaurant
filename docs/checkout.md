# Group 5: Orders, Checkout, Payment Reconciliation

## Scope

- Checkout previews calculated by Go from the active cart and a delivery address belonging to the logged-in customer.
- Transactional, idempotent order creation. An `Idempotency-Key` HTTP header is required. The user row is locked before checking this key or creating an order. The cart row is locked, meal prices and modifiers are validated again and snapshot into `orders` and `order_items`, and the cart is cleared in the same database transaction.
- Order items, selected modifier names/prices, and the delivery address are immutable snapshots. Later menu/address edits do not rewrite the existing order's commercial record.
- Money is integer minor units. Initial checkout currently supports **AED only**. `DELIVERY_FEE_MINOR` defaults to zero and `TAX_RATE_BPS` defaults to zero. A merchant must set the correct fees and tax policy before serving real customers. Checkout tax uses `(subtotal + delivery) * TAX_RATE_BPS / 10000`, truncated to minor units.
- COD (cash on delivery): a placed order remains **unpaid**. COD is configurable using `ALLOW_COD`. Cash collection, rider assignment, refunds and operations dashboards are not included.
- Optional Stripe Checkout: requires **both** `STRIPE_SECRET_KEY` and `STRIPE_WEBHOOK_SECRET`. The server creates a hosted Checkout Session with an order-specific Stripe idempotency key. It never accepts client-supplied price totals or redirect URLs. Online orders begin `pending_payment` / `pending`.
- Signed Stripe webhooks, using a five-minute tolerance and HMAC-SHA256 `Stripe-Signature`, reconcile `checkout.session.completed` (only if `payment_status=paid`), `checkout.session.async_payment_succeeded`, `checkout.session.async_payment_failed`, and `checkout.session.expired`. The API checks the stored Stripe session ID, customer reference/order metadata, amount and currency. Event IDs are unique in PostgreSQL to prevent duplicate processing. A paid order cannot be downgraded by a late failure.
- Return redirects **never** mark an order as paid. A server-side verified webhook is required.
- Checkout uses Stripe-hosted checkout and does not store card data. Minimum Stripe charge configured for AED is 200 fils.

## API

All customer routes require a session. All checkout mutations require the exact configured Origin. Stripe webhooks use their Stripe signature, not a browser session.

| Method | Endpoint | Purpose |
|---|---|---|
| GET | /api/v1/checkout/config | Available COD and Stripe payment methods |
| GET | /api/v1/checkout/quote?address_id={UUID} | Validated subtotal, tax, delivery fee and total |
| POST | /api/v1/checkout | Create/retry an idempotent order and initiate payment |
| GET | /api/v1/orders | Last 50 orders owned by the customer |
| GET | /api/v1/orders/{UUID} | Full owned order snapshot |
| POST | /api/v1/webhooks/stripe | Verified Stripe event reconciliation |

Checkout JSON:

```json
{
  "address_id": "a61e85d0-2870-4dcf-8a2c-85dc56c2a139",
  "payment_method": "cod",
  "notes": "Leave at reception"
}
```

Send `Idempotency-Key: order_<random_uuid>` with the checkout POST. The same authenticated customer and idempotency key returns the original order; changing address/payment method/notes while reusing the key returns 409. The key is scoped to a user by a unique PostgreSQL index. On transient Stripe errors, retry **the same key**; generating a new key cannot recover a previously created pending-payment order after the cart was consumed.

## Set up Stripe (optional)

1. Configure a supported Stripe account and obtain test-mode `STRIPE_SECRET_KEY` and `STRIPE_WEBHOOK_SECRET`. For UAE Stripe accounts, Stripe Checkout and major card/wallet payment methods are available.
2. Register `https://YOUR-API-HOST/api/v1/webhooks/stripe` as your Stripe webhook endpoint and subscribe to the four events listed above.
3. Run behind HTTPS with matching `API_PUBLIC_URL` and `WEB_ORIGIN`. Set your own delivery/tax rules in `.env`.
4. Test in Stripe test mode before provisioning live credentials; no real payment keys are committed to the repository.
5. The Stripe request uses a **single total-priced line item**, representing the snapshotted order, with its own order ID in provider metadata. Prices are therefore not itemized on the Stripe-hosted checkout page; they are itemized in the application's order detail view.

## Test

The CI suite runs Go race-enabled tests with PostgreSQL, frontend lint/typecheck/build and Stripe Checkout mock requests. A live Stripe account and webhook endpoint are not exercised in CI.

## Remaining before production

- Support delivery-zone validation, restaurant operating hours, real address geocoding and inventory reservations.
- Validate merchant VAT registration and applicable regional taxes; defaults are placeholders, not tax advice.
- Stripe test/live-mode browser E2E; payment timeouts, expired sessions, reconciliation polling and order cancellation/void/refund flow.
- Operational fulfillment workflow (Group 7), merchant order dashboard (Group 8), notification delivery.
- Reconcile order/payment state transitions with a documented cancellation policy; add scheduled reconciliation to check pending-payment orders.
- Persist Stripe webhook event receipts (implemented) and define audit retention and personal-data controls.
- Enforce catalog mutation locking or SERIALIZABLE snapshot/retry for exceptionally high concurrency during checkout before accepting actual payments. This group recalculates every line in one checkout transaction but does not reserve inventory.
