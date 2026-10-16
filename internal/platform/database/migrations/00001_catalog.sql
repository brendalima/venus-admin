-- +goose Up

-- Keeps updated_at correct no matter which query touched the row. The
-- "unpublished changes" badge depends on this column, so it cannot be
-- left to whoever writes the UPDATE.
-- +goose StatementBegin
CREATE FUNCTION set_updated_at() RETURNS TRIGGER AS $$
BEGIN
  NEW.updated_at = now();
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TABLE products (
  id          BIGSERIAL PRIMARY KEY,
  slug        TEXT NOT NULL UNIQUE,
  name        TEXT NOT NULL CHECK (length(trim(name)) > 0),
  -- Integer cents, never a float: 29.90 * 3 in floating point yields
  -- 89.69999999999999. The admin takes reais and converts at the edge.
  price_cents BIGINT NOT NULL CHECK (price_cents >= 0),
  sizes       TEXT[] NOT NULL DEFAULT '{}',
  description TEXT NOT NULL DEFAULT '',
  details     TEXT[] NOT NULL DEFAULT '{}',
  sold_out    BOOLEAN NOT NULL DEFAULT FALSE,
  position    INTEGER NOT NULL,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX products_position_idx ON products (position);

CREATE TRIGGER products_set_updated_at
  BEFORE UPDATE ON products
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE product_images (
  id          BIGSERIAL PRIMARY KEY,
  product_id  BIGINT NOT NULL REFERENCES products(id) ON DELETE CASCADE,
  label       TEXT NOT NULL,
  url_600     TEXT NOT NULL,
  url_1200    TEXT NOT NULL,
  r2_key_600  TEXT NOT NULL,
  r2_key_1200 TEXT NOT NULL,
  position    INTEGER NOT NULL,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- The first image is the cover, so ordering is always read together
-- with the product.
CREATE INDEX product_images_product_position_idx
  ON product_images (product_id, position);

-- Records when the site was last published, to compare against each
-- product's updated_at and show whether a change is pending.
-- The boolean primary key with a CHECK allows at most one row.
CREATE TABLE site_state (
  id                BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (id),
  last_published_at TIMESTAMPTZ
);

INSERT INTO site_state (id, last_published_at) VALUES (TRUE, NULL);

-- +goose Down
DROP TABLE site_state;
DROP TABLE product_images;
DROP TABLE products;
DROP FUNCTION set_updated_at;
