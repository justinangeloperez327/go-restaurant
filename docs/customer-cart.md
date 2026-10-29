# Group 4 — Customer profiles, delivery addresses and shopping cart

## Delivered
Authenticated users have a personal profile and a maximum of 15 saved delivery addresses. One address per user is the default; the API promotes another saved address when the default is deleted. Address updates/deletes always include the authenticated user ID in SQL predicates. All cookie-authenticated mutations require the exact configured Origin and reject extra JSON fields or documents.

The shopping cart is one restaurant per user, backed by PostgreSQL. Adding a meal from a different restaurant produces HTTP 409 until the existing cart is cleared. Add, change quantity, delete line and clear all operations are supported. Limits: 40 lines, 100 total units, 50 per line, 30 distinct modifier options per line. Cart write transactions serialize on the user-owned cart row. Foreign keys enforce restaurant consistency between the cart, its items and selected modifiers.

The **server**, not the browser, validates that restaurants, categories and meals are published and that meals are available. Required modifier groups must satisfy min/max selections; options must be in the correct meal and restaurant and available. Duplicate modifiers are rejected. Amounts use integer minor currency units and are recalculated from the live menu when the cart is read. If a meal or modifier becomes unavailable, it remains visible but `is_valid` becomes false and `can_checkout` becomes false. The customer removes and re-adds it. Adding a published, sold-out meal is rejected.

## HTTP API

| Method | Route | Description |
|---|---|---|
| GET | /api/v1/me/profile | Current customer's profile, including authenticated email |
| PUT | /api/v1/me/profile | Update `full_name`, `phone` |
| GET | /api/v1/me/addresses | All saved addresses, default first |
| POST | /api/v1/me/addresses | Create address |
| PUT | /api/v1/me/addresses/{addressID} | Replace owned address |
| DELETE | /api/v1/me/addresses/{addressID} | Remove owned address |
| GET | /api/v1/cart | Repriced cart view, line validity and subtotal |
| POST | /api/v1/cart/items | Add meal, quantity, selected options |
| PUT | /api/v1/cart/items/{itemID} | Update quantity |
| DELETE | /api/v1/cart/items/{itemID} | Remove line |
| DELETE | /api/v1/cart | Clear cart and release restaurant selection |

All routes require a valid session. PUT and POST require exactly one JSON document with `Content-Type: application/json`. Cross-origin cookie mutations without an allowed Origin receive 403.

Add-to-cart example:

```json
{ "meal_id": "a61e85d0-2870-4dcf-8a2c-85dc56c2a139",
  "quantity": 2,
  "option_ids": ["24b3ae69-7c52-4100-903b-9c21dd97cbfd"] }
```

Cart amounts are informational, not a financial authorization. **Group 5 checkout must revalidate the selected modifiers and snapshot final prices transactionally**, apply taxes and delivery charges, and require idempotency keys before creating payments or orders. Never charge or fulfill solely on cart values.

## Web UI

- `/account`: edit name, phone and delivery addresses.
- `/restaurants/{id}`: modifier choice and add-to-cart controls.
- `/cart`: owned cart lines, quantity controls, remove/clear, server subtotal and stale-item notices.
- Home links expose account and cart navigation.

The current auth setup requires real Google/Apple developer credentials, so manual sign-in testing needs a configured provider.

## Production follow-ups

- Profile and address APIs should eventually support verified phone/contact changes and localized address/postcode validation for the country serviced; `country_code` stores ISO-style uppercase two-character codes without additional country-specific validation.
- Checkout will need delivery-zone validation, selected delivery address ownership checks, calculated taxes, reservation of inventory if applicable and order price snapshots.
- Cart modifiers store option IDs only, and a deleted modifier may disappear from the selected options due to FK cascading; checkout must never trust old cart selection.
- Rate limits and appropriate audit events should cover customer mutations before an external public release.
- If immediate catalog changes must invalidate in-flight cart additions, lock catalog rows consistently in both catalog mutations and cart writes. The read path always reprices the currently committed menu; no payment is processed in Group 4.
