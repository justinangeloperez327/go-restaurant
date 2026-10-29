-- Group 7: one delivery job per paid/COD-eligible placed order.
-- No external maps or rider locations are disclosed in public endpoints.
CREATE TABLE delivery_jobs (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  order_id uuid NOT NULL UNIQUE REFERENCES orders(id) ON DELETE RESTRICT,
  restaurant_id uuid NOT NULL REFERENCES restaurants(id) ON DELETE RESTRICT,
  rider_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
  status text NOT NULL DEFAULT 'assigned'
    CHECK (status IN ('assigned','picked_up','en_route','delivered','failed')),
  assigned_at timestamptz NOT NULL DEFAULT now(),
  picked_up_at timestamptz,
  en_route_at timestamptz,
  completed_at timestamptz,
  failure_reason text NOT NULL DEFAULT '' CHECK (char_length(failure_reason) <= 250),
  latitude numeric(9,6) CHECK (latitude BETWEEN -90 AND 90),
  longitude numeric(9,6) CHECK (longitude BETWEEN -180 AND 180),
  location_accuracy_m numeric(8,2) CHECK (location_accuracy_m BETWEEN 0 AND 1000),
  location_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT delivery_coordinate_pair CHECK (
    (latitude IS NULL AND longitude IS NULL AND location_accuracy_m IS NULL AND location_at IS NULL)
    OR (latitude IS NOT NULL AND longitude IS NOT NULL AND location_accuracy_m IS NOT NULL AND location_at IS NOT NULL)
  )
);
CREATE INDEX delivery_jobs_by_rider ON delivery_jobs(rider_id,status,assigned_at DESC);
CREATE INDEX delivery_jobs_by_restaurant ON delivery_jobs(restaurant_id,status,created_at DESC);

CREATE TABLE delivery_events (
  seq bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  delivery_id uuid NOT NULL REFERENCES delivery_jobs(id) ON DELETE RESTRICT,
  event_type text NOT NULL CHECK (
    event_type IN ('assigned','reassigned','picked_up','en_route','delivered','failed','location')
  ),
  actor_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX delivery_events_by_delivery ON delivery_events(delivery_id,seq);
