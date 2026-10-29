-- Group 5: immutable order and payment reconciliation records.
CREATE TABLE orders (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
  restaurant_id uuid NOT NULL REFERENCES restaurants(id) ON DELETE RESTRICT,
  address_id uuid REFERENCES delivery_addresses(id) ON DELETE SET NULL,
  delivery_address jsonb NOT NULL,
  notes text NOT NULL DEFAULT '' CHECK (char_length(notes) <= 500),
  idempotency_key text NOT NULL CHECK (char_length(idempotency_key) BETWEEN 12 AND 128),
  payment_method text NOT NULL CHECK (payment_method IN ('cod','stripe')),
  order_status text NOT NULL CHECK (order_status IN ('pending_payment','placed','payment_failed','cancelled')),
  payment_status text NOT NULL CHECK (payment_status IN ('unpaid','pending','paid','failed')),
  currency char(3) NOT NULL CHECK (currency='AED'),
  subtotal_minor bigint NOT NULL CHECK (subtotal_minor >= 0),
  delivery_fee_minor bigint NOT NULL CHECK (delivery_fee_minor >= 0),
  tax_minor bigint NOT NULL CHECK (tax_minor >= 0),
  total_minor bigint NOT NULL CHECK (total_minor >= 0),
  stripe_session_id text UNIQUE,
  stripe_checkout_url text NOT NULL DEFAULT '',
  stripe_payment_intent_id text,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(user_id,idempotency_key),
  CHECK (total_minor = subtotal_minor + delivery_fee_minor + tax_minor)
);
CREATE INDEX orders_by_customer ON orders(user_id,created_at DESC,id);
CREATE INDEX orders_by_restaurant ON orders(restaurant_id,created_at DESC,id);

CREATE TABLE order_items (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  order_id uuid NOT NULL REFERENCES orders(id) ON DELETE RESTRICT,
  meal_id uuid REFERENCES meals(id) ON DELETE SET NULL,
  meal_name text NOT NULL,
  quantity integer NOT NULL CHECK (quantity BETWEEN 1 AND 50),
  base_price_minor bigint NOT NULL CHECK (base_price_minor >= 0),
  unit_price_minor bigint NOT NULL CHECK (unit_price_minor >= 0),
  line_total_minor bigint NOT NULL CHECK (line_total_minor = unit_price_minor*quantity),
  selected_modifiers jsonb NOT NULL DEFAULT '[]'::jsonb
);
CREATE INDEX order_items_order_idx ON order_items(order_id,id);

CREATE TABLE payment_webhook_events (
  provider text NOT NULL CHECK (provider='stripe'),
  event_id text NOT NULL,
  event_type text NOT NULL,
  order_id uuid NOT NULL REFERENCES orders(id) ON DELETE RESTRICT,
  received_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY(provider,event_id)
);
