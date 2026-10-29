# Group 3 — Restaurants, categories, meals and modifiers

## Architecture

Restaurant and menu data are stored in PostgreSQL. The API is a Go/Chi modular monolith. Restaurant managers must have BOTH the `restaurant_manager` role with `catalog.manage` and an explicit `restaurant_staff` assignment for the target restaurant. Admins can manage every restaurant and create or archive restaurants.

RBAC and assignment are checked in the SQL statement for each catalog mutation. Restaurant-manager assignments require an active user with the `restaurant_manager` role and can be changed only by an administrator. Unassigned managers and customers cannot mutate catalogs. Client-side UI visibility is not security authorization.

## Core model

- `restaurants`: name, unique lowercase slug, description, fixed three-character currency, HTTPS image URL, publication status.
- `restaurant_staff`: explicit restaurant-to-user manager assignment.
- `menu_categories`: name, display order, publication status.
- `meals`: category reference, name, description, integer price in **minor currency units**, image URL, publication flag, independent availability flag and order.
- `modifier_groups`: per-meal group and min/max selections (0–30).
- `modifier_options`: named selectable option, nonnegative extra price, availability and display order.

The database enforces composite foreign keys so a meal cannot use a category from another restaurant. Modifier groups/options cannot cross restaurant boundaries. Categories with existing meals cannot be deleted. Deleting a meal deletes its modifier groups and options. Archive restaurants rather than deleting them; orders and subscriptions will reference them in later groups.

## Routes

Public (no login):
- `GET /api/v1/restaurants` — published restaurants.
- `GET /api/v1/restaurants/{restaurantID}/menu` — published categories, published meals (including sold-out meals) and their options. Never includes draft menus.

Authenticated:
- `GET /api/v1/admin/restaurants/` — restaurants in the user's scope.
- `GET /api/v1/admin/restaurants/{restaurantID}/catalog` — complete menu, including drafts.
- `POST /api/v1/admin/restaurants/` — admin only.
- `PUT /api/v1/admin/restaurants/{restaurantID}` — scoped manager/admin; currency is immutable.
- `DELETE /api/v1/admin/restaurants/{restaurantID}` — admin only; archive, not hard deletion.
- `PUT|DELETE /api/v1/admin/restaurants/{restaurantID}/managers/{userID}` — admin only.
- `POST /api/v1/admin/restaurants/{restaurantID}/categories/` and `PUT|DELETE .../categories/{categoryID}`.
- `POST /api/v1/admin/restaurants/{restaurantID}/meals/` and `PUT|DELETE .../meals/{mealID}`.
- `POST /api/v1/admin/restaurants/{restaurantID}/meals/{mealID}/modifier-groups/` and `PUT|DELETE .../modifier-groups/{groupID}`.
- `POST /api/v1/admin/restaurants/{restaurantID}/meals/{mealID}/modifier-groups/{groupID}/options/` and `PUT|DELETE .../options/{optionID}`.

All write endpoints require a logged-in session and exact configured browser Origin. POST and PUT require application/json bodies; PUT is **full replacement**, not partial PATCH. Responses use 201 for creations, 200 for updates, 204 for removals, 400 for invalid fields, 401 for missing login, 403 for unauthorized/missing resources, and 409 for foreign key or uniqueness conflicts. IDs are UUIDs.

## Example

```json
{
  "category_id": "a61e85d0-2870-4dcf-8a2c-85dc56c2a139",
  "name": "Grilled chicken bowl",
  "description": "Rice and seasonal vegetables",
  "price_minor": 2450,
  "image_url": "https://images.example.com/grilled-chicken.jpg",
  "is_active": true,
  "is_available": true,
  "sort_order": 10
}
```

A price of `2450` in AED represents **AED 24.50**. Do not use floating-point amounts in APIs or the database. All image URLs must use HTTPS. No image-upload service is included yet: store image URLs from a trusted media host. The public frontend escapes text; browser image resources still involve third-party requests.

## Admin UI

Navigate to `/admin/catalog` after logging in. Admin users can create restaurants and assign restaurant managers by UUID. Scoped managers can update restaurant settings, categories, meals, groups and options. To publish items, mark their restaurant, category and meal active; `is_available` independently controls sold-out status.

There is no sample catalog seeded. Bootstrap an admin in a trusted database session per `docs/auth.md`, then create a restaurant manually.

## Limitations / production notes

- Images use validated external HTTPS URLs, not uploads or a media CDN.
- No cart, orders, checkout, inventory or scheduled availability yet; those belong to later groups.
- Money assumes two decimal minor units for the currently supported AED-oriented UI. Currency-specific decimal exponents will be handled before broadening currencies or payment handling.
- The public menu presents modifiers for inspection, but choice enforcement belongs in the checkout group. A mandatory group with too few available options cannot be fulfilled; publish sensible selections.
- For a large restaurant catalog, add pagination and cache headers/CDN caching with appropriate publication invalidation before production.
