-- Group 8: restaurant-scoped operational alerts and per-user read receipts.
-- Alerts contain event metadata only (no addresses, phone numbers or GPS).
CREATE TABLE operation_alerts (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 restaurant_id uuid NOT NULL REFERENCES restaurants(id) ON DELETE RESTRICT,
 kind text NOT NULL CHECK (kind IN ('order_placed','payment_received','payment_failed','delivery_failed','subscription_failed')),
 source_id uuid NOT NULL,
 title text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 120),
 created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(restaurant_id,kind,source_id)
);
CREATE INDEX operation_alerts_recent ON operation_alerts(restaurant_id,created_at DESC,id);
CREATE TABLE operation_alert_reads (
 alert_id uuid NOT NULL REFERENCES operation_alerts(id) ON DELETE CASCADE,
 user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 read_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(alert_id,user_id)
);
CREATE INDEX operation_alert_reads_user ON operation_alert_reads(user_id,read_at DESC);
