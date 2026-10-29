# Group 6 — Weekly meal subscriptions

## Model and capabilities

Group 6 adds **opt-in recurring COD delivery orders**, not automatic Stripe charges. A customer can save the active cart as a weekly meal template. The cart is not consumed. Each selected weekday (Sunday=0 through Saturday=6) has the same delivery time, saved address, quantity and modifier selections. Schedules use the fixed UAE **Asia/Dubai UTC+04** timezone with no DST and a 24-hour lead time. Initial start date must be within 60 days; the earliest eligible occurrence must have a cutoff in the future.

Subscriptions generate **one COD order per delivery**. Every occurrence is repriced and checked against current published/available meals, modifier requirements, the saved delivery address and the customer-set **maximum total**. Orders are snapshotted transactionally, preserving names, quantity, modifiers, fees, tax, address and planned delivery timestamp. Orders are always `unpaid` until operational cash collection is implemented. The worker generates orders up to 24 hours before scheduled delivery and never backdates deliveries after their delivery time. If the meal becomes unavailable, the address is deleted, or a new price exceeds the maximum, it records a failed occurrence without creating a charge or order.

The 7-day subscription period renews automatically every week only when `auto_renew=true`. Disabling auto-renew stops the subscription after the current seven-day period. Pausing stops future generation and resuming jumps forward to the next cutoff-safe delivery. Cancelling is permanent for future generation. Already created COD orders remain separate and are not cancelled by pausing or cancelling a subscription.

The Go API starts a bounded polling worker every minute (maximum 50 due occurrences per pass). Each transaction claims a single due subscription using `FOR UPDATE SKIP LOCKED`. The unique `(subscription_id, delivery_date)` and unique `(subscription_id, delivery_scheduled_at)` constraints prevent duplicate delivery/order creation across replicas. A stable idempotency key is also embedded in the generated order.

## Routes

All routes require an authenticated cookie session; mutations require the configured browser Origin.

| Method | Path | Purpose |
|---|---|---|
| GET | /api/v1/subscriptions | List owned subscriptions |
| POST | /api/v1/subscriptions | Create from current cart |
| GET | /api/v1/subscriptions/{id} | Subscription settings |
| PUT | /api/v1/subscriptions/{id} | Set auto-renew and max total in fils |
| POST | /api/v1/subscriptions/{id}/pause | Pause |
| POST | /api/v1/subscriptions/{id}/resume | Resume |
| POST | /api/v1/subscriptions/{id}/cancel | Cancel |
| POST | /api/v1/subscriptions/{id}/skips | Skip a specific scheduled date before cutoff |
| GET | /api/v1/subscriptions/{id}/occurrences | Last 100 delivery results |

Creation JSON:

```json
{
  "address_id": "a61e85d0-2870-4dcf-8a2c-85dc56c2a139",
  "start_date": "2026-11-02",
  "delivery_time": "12:00",
  "weekdays": [1, 3, 5],
  "max_total_minor": 6000,
  "auto_renew": true,
  "notes": "Leave at reception"
}
```

The `start_date` must be in the future as evaluated at request time, **not the fixed sample above**. The cap is **AED fils**: `6000` is AED 60.00, and must be at least the current menu total. Use `POST /api/v1/subscriptions/{id}/skips` with `{"date":"YYYY-MM-DD"}` to skip a selected future weekday. No skip is accepted after its 24-hour lead-time cutoff.

## Limitations and safeguards

- **No recurring card billing, prepaid wallets, automatic refunds, or Stripe subscription products.** COD is the only supported recurring payment method. Existing one-time Stripe Checkout remains unchanged.
- The worker uses the API process; it is safe across replicas but stops if no API instances are running. For operational deployment, move it into a dedicated monitored worker and add alerting for failed occurrences.
- Scheduled deliveries are **order creation**, not physical fulfillment or rider dispatch. Those follow in Group 7.
- There is no inventory reservation, holiday calendar, service-area geocoding, daily capacity or merchant confirmation. Do not enable actual delivery operations without these.
- Real-time pricing and price caps mean future meals may be skipped as failed occurrences if prices increase. No undisclosed higher COD amount is generated.
- The worker's generated orders use current configured fees and tax; merchants need to validate VAT/fee rules and local consumer obligations before launch.
- Subscription cancellation does not retroactively cancel orders already created. Add order cancellation/refund workflows and notifications in later groups.
