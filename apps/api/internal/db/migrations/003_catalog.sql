-- Catalog schemas. All money is stored as integer minor units (fils for AED).
INSERT INTO permissions (name) VALUES ('restaurants.manage') ON CONFLICT DO NOTHING;
INSERT INTO role_permissions (role_name, permission_name)
VALUES ('admin','restaurants.manage') ON CONFLICT DO NOTHING;

CREATE TABLE restaurants (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 140),
  slug text NOT NULL UNIQUE CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'),
  description text NOT NULL DEFAULT '',
  currency char(3) NOT NULL DEFAULT 'AED' CHECK (currency ~ '^[A-Z]{3}$'),
  image_url text NOT NULL DEFAULT '',
  is_active boolean NOT NULL DEFAULT false,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE restaurant_staff (
  restaurant_id uuid NOT NULL REFERENCES restaurants(id) ON DELETE CASCADE,
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (restaurant_id, user_id)
);
CREATE INDEX restaurant_staff_user_idx ON restaurant_staff (user_id);

CREATE TABLE menu_categories (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  restaurant_id uuid NOT NULL REFERENCES restaurants(id) ON DELETE CASCADE,
  name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 100),
  sort_order integer NOT NULL DEFAULT 0,
  is_active boolean NOT NULL DEFAULT false,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(restaurant_id, id)
);
CREATE INDEX menu_categories_restaurant_idx ON menu_categories (restaurant_id, sort_order, id);

CREATE TABLE meals (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  restaurant_id uuid NOT NULL REFERENCES restaurants(id) ON DELETE CASCADE,
  category_id uuid NOT NULL,
  name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 140),
  description text NOT NULL DEFAULT '',
  price_minor bigint NOT NULL CHECK (price_minor >= 0 AND price_minor <= 1000000000),
  image_url text NOT NULL DEFAULT '',
  is_active boolean NOT NULL DEFAULT false,
  is_available boolean NOT NULL DEFAULT true,
  sort_order integer NOT NULL DEFAULT 0,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(restaurant_id, id),
  CONSTRAINT meals_category_fk FOREIGN KEY (restaurant_id, category_id)
    REFERENCES menu_categories(restaurant_id,id) ON DELETE RESTRICT
);
CREATE INDEX meals_menu_idx ON meals (restaurant_id,category_id,sort_order,id);

CREATE TABLE modifier_groups (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  restaurant_id uuid NOT NULL,
  meal_id uuid NOT NULL,
  name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 100),
  min_select integer NOT NULL DEFAULT 0 CHECK (min_select >= 0),
  max_select integer NOT NULL DEFAULT 1 CHECK (max_select >= 1),
  sort_order integer NOT NULL DEFAULT 0,
  created_at timestamptz NOT NULL DEFAULT now(),
  CHECK (min_select <= max_select AND max_select <= 30),
  UNIQUE(restaurant_id,id),
  CONSTRAINT group_meal_fk FOREIGN KEY (restaurant_id,meal_id)
    REFERENCES meals(restaurant_id,id) ON DELETE CASCADE
);
CREATE INDEX modifier_groups_meal_idx ON modifier_groups (restaurant_id,meal_id,sort_order,id);

CREATE TABLE modifier_options (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  restaurant_id uuid NOT NULL,
  group_id uuid NOT NULL,
  name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 100),
  price_delta_minor bigint NOT NULL DEFAULT 0 CHECK (price_delta_minor BETWEEN 0 AND 1000000000),
  is_available boolean NOT NULL DEFAULT true,
  sort_order integer NOT NULL DEFAULT 0,
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT option_group_fk FOREIGN KEY (restaurant_id,group_id)
    REFERENCES modifier_groups(restaurant_id,id) ON DELETE CASCADE
);
CREATE INDEX modifier_options_group_idx ON modifier_options (restaurant_id,group_id,sort_order,id);
