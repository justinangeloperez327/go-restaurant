-- Customer profile, owned delivery addresses and a single-restaurant cart.
-- Money remains sourced exclusively from the live catalog; no client prices are stored.
CREATE TABLE customer_profiles (
  user_id uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  full_name text NOT NULL DEFAULT '' CHECK (char_length(full_name) <= 140),
  phone text NOT NULL DEFAULT '' CHECK (char_length(phone) <= 24),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE delivery_addresses (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  label text NOT NULL CHECK (char_length(btrim(label)) BETWEEN 1 AND 40),
  recipient_name text NOT NULL CHECK (char_length(btrim(recipient_name)) BETWEEN 1 AND 140),
  phone text NOT NULL CHECK (char_length(btrim(phone)) BETWEEN 7 AND 24),
  country_code char(2) NOT NULL CHECK (country_code ~ '^[A-Z]{2}$'),
  city text NOT NULL CHECK (char_length(btrim(city)) BETWEEN 1 AND 100),
  area text NOT NULL CHECK (char_length(area) <= 150),
  street text NOT NULL CHECK (char_length(btrim(street)) BETWEEN 1 AND 200),
  building text NOT NULL CHECK (char_length(building) <= 120),
  apartment text NOT NULL CHECK (char_length(apartment) <= 80),
  instructions text NOT NULL CHECK (char_length(instructions) <= 500),
  is_default boolean NOT NULL DEFAULT false,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(user_id,id)
);
CREATE INDEX delivery_addresses_owner_idx ON delivery_addresses (user_id,created_at,id);
CREATE UNIQUE INDEX delivery_addresses_one_default ON delivery_addresses(user_id) WHERE is_default;

CREATE TABLE customer_carts (
  user_id uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  restaurant_id uuid NOT NULL REFERENCES restaurants(id) ON DELETE RESTRICT,
  updated_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE customer_carts ADD CONSTRAINT customer_carts_owner_restaurant_key UNIQUE(user_id,restaurant_id);
CREATE TABLE cart_items (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL,

  restaurant_id uuid NOT NULL,
  meal_id uuid NOT NULL,
  quantity integer NOT NULL CHECK (quantity BETWEEN 1 AND 50),
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(id,restaurant_id),
  FOREIGN KEY (restaurant_id,meal_id) REFERENCES meals(restaurant_id,id) ON DELETE CASCADE,
  FOREIGN KEY (user_id,restaurant_id) REFERENCES customer_carts(user_id,restaurant_id) ON DELETE CASCADE
);
CREATE INDEX cart_items_owner_idx ON cart_items(user_id,created_at,id);

-- Enforce that every selected modifier belongs to the same restaurant as its cart item.
ALTER TABLE modifier_options ADD CONSTRAINT modifier_options_restaurant_id_unique UNIQUE(restaurant_id,id);
CREATE TABLE cart_item_options (
  cart_item_id uuid NOT NULL,
  restaurant_id uuid NOT NULL,
  option_id uuid NOT NULL,
  PRIMARY KEY (cart_item_id,option_id),
  FOREIGN KEY (cart_item_id,restaurant_id) REFERENCES cart_items(id,restaurant_id) ON DELETE CASCADE,
  FOREIGN KEY (restaurant_id,option_id) REFERENCES modifier_options(restaurant_id,id) ON DELETE CASCADE
);
