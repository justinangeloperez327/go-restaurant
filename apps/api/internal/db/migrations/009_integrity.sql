-- Group 9 additive integrity constraints and hot-path indexes.
ALTER TABLE orders
 ADD CONSTRAINT orders_address_json_object CHECK (jsonb_typeof(delivery_address) = 'object');
ALTER TABLE order_items
 ADD CONSTRAINT order_items_modifiers_json_array CHECK (jsonb_typeof(selected_modifiers) = 'array');
CREATE INDEX orders_pending_stripe_reconcile_idx ON orders(updated_at,id)
 WHERE payment_method='stripe' AND payment_status='pending';
CREATE INDEX orders_unassigned_dispatch_idx ON orders(restaurant_id,created_at DESC,id)
 WHERE order_status='placed';
CREATE INDEX delivery_events_timeline_idx ON delivery_events(delivery_id,created_at DESC);
