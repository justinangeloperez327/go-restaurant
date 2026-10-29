# Group 8 — Operational dashboard, reports, and in-app alerts

## Scope
The Group 8 dashboard is for **restaurant managers assigned to their restaurant** and **administrators**. Server-side authorization checks active roles and restaurant_staff memberships for each requested restaurant. A customer, rider, or manager of another restaurant cannot retrieve reporting or alerts.

Available at `/admin/operations`. The new API resources:
- `GET /api/v1/admin/operations/report?restaurant_id={UUID}&from=YYYY-MM-DD&to=YYYY-MM-DD`
- `GET /api/v1/admin/operations/report.csv?restaurant_id={UUID}&from=YYYY-MM-DD&to=YYYY-MM-DD`
- `GET /api/v1/admin/operations/alerts?restaurant_id={UUID}`
- `PUT /api/v1/admin/operations/alerts/{alertID}/read?restaurant_id={UUID}` (requires exact configured browser Origin)

The reporting window is inclusive of both dates, at most **90 calendar days**, and dates use **Asia/Dubai** boundaries. Daily values use AED **fils**, and the dashboard converts to AED for display. CSV exports integer minor units without rounding ambiguity. The report is read from a consistent PostgreSQL REPEATABLE READ transaction.

### KPI definitions

- **Orders**: orders created during the chosen period.
- **Placed orders / booked value**: order_status=placed, using immutable checkout totals. Booked value is **not cash collected or recognized revenue**.
- **Verified online payments**: orders with payment_status=paid and created during the period; separate from booked value.
- **Uncollected COD**: placed COD orders still marked unpaid, using their snapshotted total. Cash reconciliation has not been implemented; this is not an accounts-receivable ledger.
- **Pending and unsuccessful orders**: currently pending_payment, failed or cancelled, within the creation window.
- **Delivery status counts**: jobs assigned/started/completed/failed, filtered by job creation date. Counts represent current state, not historical state as of the date.
- **Active/paused subscriptions**: current counts, not filtered by creation dates.
- **Subscription failures**: failed delivery occurrences recorded in the selected period.
- **Unassigned orders**: current eligible placed orders without delivery_jobs, not limited to the selected reporting dates. This is an operational backlog measure.

Metrics of different grains (current backlog vs within-period order events) are clearly differentiated in the UI. Do not sum booked value, verified receipts and COD uncollected.

### Operational alerts

A background reconciler runs at process startup and once per minute, selecting at most 1,000 recent source records per kind each pass from the previous 30 days. It inserts deterministic, unique `(restaurant_id,kind,source_id)` records for:
- Placed orders
- Verified Stripe payments
- Payment failures
- Failed deliveries
- Failed subscription occurrences

Separate per-user read receipts allow two managers to independently acknowledge alerts without altering each other's inbox. Reads are scoped by restaurant ID and active current permissions. Alert payloads never include addresses, phone numbers, or GPS coordinates. REST polling refreshes the dashboard and alerts once per 30 seconds; this group does not add push/email/SMS providers or new infrastructure.

Migration `008_operations.sql` adds operation_alerts and operation_alert_reads with indexes and source-event deduplication. There is no auto-deployment; CI uses Go 1.27.3, Node.js 24 and PostgreSQL integration tests.

### Limitations for production

- Reporting is not a settlement or accounting ledger: no refunds or cash collection reconciliation yet, no delivery SLA or inventory adjustment, no VAT invoice outputs.
- Alert delivery is **in-app only** and may be delayed by one minute or longer while all API instances are stopped. Backfill is limited to 30 days.
- For large deployments, move the alert scanner into a dedicated monitored worker, use outbox events to avoid scans and add per-user pagination/retention rules.
- No external notification provider credentials are required or configured.
