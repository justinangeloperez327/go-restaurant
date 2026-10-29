# Group 7 — Dispatch, Rider Delivery Status and Live GPS Tracking

This release extends the existing **Go modular monolith**, PostgreSQL 18 and Next.js frontend. It does not add Redis, Kafka or a separate tracking service.

## State and access control

A placed order may receive exactly one delivery job. COD must remain unpaid and Stripe orders must be verified paid before dispatch. A manager needs `restaurant_manager` + `orders.manage` and a matching `restaurant_staff` assignment. Admins need `delivery.manage`. Only active users with `rider` + `delivery.manage` can be assigned. Assignment/reassignment is serialized by an order row lock. Reassignment is allowed only before pickup; prior GPS coordinates are immediately deleted.

The assigned rider can transition a job: **assigned → picked_up → en_route → delivered**. Any active state can transition to **failed** with a reason. No invalid, backward or repeated transitions are permitted. A terminal job cannot be modified or reassigned; coordinates are cleared at completion/failure. A customer can read only deliveries for their own orders. Riders read only their own assignments. Scoped restaurant managers/admins read restaurant deliveries, subject to current role and tenant checks.

## Browser pages

- `/admin/deliveries` — restaurant dispatch: list eligible orders, select an active rider, assign/reassign, view statuses.
- `/rider/deliveries` — rider assignments and status controls; optional GPS sharing requires an explicit click and browser geolocation permission.
- `/orders/{id}` — customer live status and last active rider position; a link to external OpenStreetMap is offered only when clicked.

GPS uses **HTTPS or localhost** in browsers. The rider's watchPosition updates location no more frequently than every five seconds and the server validates latitude, longitude and reported accuracy (0–1000 m). Coordinates are not included in event history, and the live position is erased when a delivery is completed or failed. Do not use this module for employment surveillance; location collection is limited to an active assigned delivery and requires a rider action.

## API routes

| Method | Endpoint | Access |
|---|---|---|
| GET | /api/v1/orders/{orderID}/delivery | Order owner |
| GET | /api/v1/deliveries/{deliveryID} | Owner, assigned rider, scoped manager/admin |
| GET | /api/v1/deliveries/{deliveryID}/events | Same access, SSE |
| GET | /api/v1/rider/deliveries | Active riders |
| PUT | /api/v1/rider/deliveries/{deliveryID}/status | Assigned active rider |
| PUT | /api/v1/rider/deliveries/{deliveryID}/location | Assigned active rider |
| GET | /api/v1/admin/deliveries?restaurant_id={UUID} | Restaurant manager/admin |
| GET | /api/v1/admin/deliveries/riders?restaurant_id={UUID} | Restaurant manager/admin |
| GET | /api/v1/admin/deliveries/eligible-orders?restaurant_id={UUID} | Restaurant manager/admin |
| PUT | /api/v1/admin/deliveries/orders/{orderID}/assignment | Restaurant manager/admin |

Example assignment JSON: `{"rider_id":"<UUID>"}`. Status JSON: `{"status":"picked_up","reason":""}`. Terminal failure requires a reason. GPS JSON: `{"latitude":24.45,"longitude":54.37,"accuracy_m":15}`. All writes require JSON and the exact configured browser Origin; all routes require an authenticated session.

### Server-Sent Events

`GET /api/v1/deliveries/{deliveryID}/events` uses native EventSource with credentials, a reconnect retry of 3 seconds, and a 55-second bounded connection. The stream emits `ready` and `delivery-update`, with monotonic event IDs. The frontend fetches a fresh authorized delivery snapshot when a notification arrives. The server checks authorization again every two seconds and drops a stream if rider assignment or manager access is revoked. **The event data contains only event type, sequence and timestamp**, never coordinates or address. For a many-rider deployment, replace database polling with PostgreSQL NOTIFY or a pub/sub backend and deploy a dedicated tracking service.

## Database and production notes

Migration `007_deliveries.sql` adds `delivery_jobs` (one per order) and `delivery_events` (append-only state-change signals). Database constraints guard identity references, coordinates and status enums. Location is the **latest point only**, not a historical trail. This limits retention and costs; event history is not a GPS archive.

This MVP has no route optimization, geocoding, dispatch notifications, proof-of-delivery signatures, cash collection reconciliation, operational SLAs, driver background-location native app, precise address navigation, map SDK, or production monitoring. Group 8 can add administrative metrics/notifications. Before production, implement per-account rate limits, monitoring, configurable GPS consent/retention policies and service-area validation. Admin and rider screens rely on OAuth sign-in configured in Group 2. No automatic deployment is configured.
