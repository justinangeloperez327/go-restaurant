-- Group 6: UAE-local weekly COD subscriptions with immutable per-delivery order snapshots.
-- No card tokenization, auto-billing or undisclosed future charges.
CREATE TABLE meal_subscriptions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
  restaurant_id uuid NOT NULL REFERENCES restaurants(id) ON DELETE RESTRICT,
  address_id uuid NOT NULL REFERENCES delivery_addresses(id) ON DELETE RESTRICT,
  weekday_mask integer NOT NULL CHECK (weekday_mask BETWEEN 1 AND 127),
  delivery_time char(5) NOT NULL CHECK (delivery_time ~ '^([01][0-9]|2[0-3]):[0-5][0-9]$'),
  next_delivery_date date NOT NULL,
  next_run_at timestamptz NOT NULL,
  current_period_end date NOT NULL,
  auto_renew boolean NOT NULL DEFAULT true,
  max_total_minor bigint NOT NULL CHECK (max_total_minor BETWEEN 1 AND 1000000000),
  status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','paused','cancelled','ended')),
  notes text NOT NULL DEFAULT '' CHECK (char_length(notes) <= 500),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX meal_subscriptions_due_idx ON meal_subscriptions(next_run_at,id) WHERE status='active';
CREATE INDEX meal_subscriptions_owner_idx ON meal_subscriptions(user_id,created_at DESC);
CREATE TABLE meal_subscription_items (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  subscription_id uuid NOT NULL REFERENCES meal_subscriptions(id) ON DELETE CASCADE,
  meal_id uuid NOT NULL REFERENCES meals(id) ON DELETE RESTRICT,
  quantity integer NOT NULL CHECK (quantity BETWEEN 1 AND 50)
);
CREATE INDEX subscription_items_by_subscription ON meal_subscription_items(subscription_id,id);
CREATE TABLE meal_subscription_item_options (
  subscription_item_id uuid NOT NULL REFERENCES meal_subscription_items(id) ON DELETE CASCADE,
  option_id uuid NOT NULL REFERENCES modifier_options(id) ON DELETE RESTRICT,
  PRIMARY KEY (subscription_item_id,option_id)
);
CREATE TABLE meal_subscription_occurrences (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  subscription_id uuid NOT NULL REFERENCES meal_subscriptions(id) ON DELETE RESTRICT,
  delivery_date date NOT NULL,
  status text NOT NULL CHECK (status IN ('created','skipped','failed')),
  order_id uuid UNIQUE REFERENCES orders(id) ON DELETE RESTRICT,
  reason text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(subscription_id,delivery_date),
  CHECK ((status='created' AND order_id IS NOT NULL) OR (status<>'created' AND order_id IS NULL))
);
CREATE INDEX subscription_occurrences_by_subscription ON meal_subscription_occurrences(subscription_id,delivery_date DESC);

CREATE TABLE meal_subscription_skips (
  subscription_id uuid NOT NULL REFERENCES meal_subscriptions(id) ON DELETE CASCADE,
  delivery_date date NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY(subscription_id,delivery_date)
);

ALTER TABLE orders ADD COLUMN subscription_id uuid REFERENCES meal_subscriptions(id) ON DELETE RESTRICT;
ALTER TABLE orders ADD COLUMN delivery_scheduled_at timestamptz;
CREATE UNIQUE INDEX orders_subscription_delivery_unique ON orders(subscription_id,delivery_scheduled_at)
  WHERE subscription_id IS NOT NULL;
