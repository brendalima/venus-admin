-- +goose Up

-- Each piece comes in one or more colours, each with its own photos and
-- its own quantity per size. IDs come from the services, as for products.
CREATE TABLE product_colours (
  id         UUID PRIMARY KEY,
  product_id UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
  name       TEXT NOT NULL CHECK (length(trim(name)) > 0),
  position   INTEGER NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX product_colours_product_position_idx ON product_colours (product_id, position);
-- The repository turns a clash here into a message on the colour's name.
CREATE UNIQUE INDEX product_colours_product_name_key ON product_colours (product_id, lower(name));

-- Stock, one row per colour and size that is counted. Sparse on purpose:
-- a colour without rows is not counted at all, and shows on the site as
-- it always has; a size without a row, while others have one, is
-- unavailable. No ID: a row is only ever found by its colour and size.
-- The sizes are a closed list in the domain (domain.AllSizes), not here,
-- so adding one stays a one-line change.
CREATE TABLE product_variations (
  colour_id  UUID NOT NULL REFERENCES product_colours(id) ON DELETE CASCADE,
  size       TEXT NOT NULL CHECK (length(trim(size)) > 0),
  quantity   INTEGER NOT NULL CHECK (quantity >= 0),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (colour_id, size)
);

-- Without these, the pending-changes badge would miss every change of
-- colour and of stock. Row-level, for the reason given in 00003.
CREATE TRIGGER product_colours_mark_site_changed
  AFTER INSERT OR UPDATE OR DELETE ON product_colours
  FOR EACH ROW EXECUTE FUNCTION mark_site_changed();

CREATE TRIGGER product_variations_mark_site_changed
  AFTER INSERT OR UPDATE OR DELETE ON product_variations
  FOR EACH ROW EXECUTE FUNCTION mark_site_changed();

-- Every existing piece gets one colour, and its photos move to it. No
-- stock rows: "no rows" means "not counted", so every piece keeps showing
-- as it does today. Rows at zero would make the whole catalogue read
-- "indisponível" on the next publish.
--
-- gen_random_uuid() does not break 00002's rule that IDs come from the
-- services: a backfill has no service to ask.
INSERT INTO product_colours (id, product_id, name, position)
SELECT gen_random_uuid(), id, 'Única', 0 FROM products;

ALTER TABLE product_images
  ADD COLUMN colour_id UUID REFERENCES product_colours(id) ON DELETE CASCADE;

UPDATE product_images i SET colour_id = c.id
FROM product_colours c
WHERE c.product_id = i.product_id;

-- Unlike 00002's guard, which refused to start on tables with rows, this
-- one refuses to finish with a photo left behind: carrying production's
-- rows across is what this migration is for. goose runs it in a
-- transaction, so raising undoes all of it.
-- +goose StatementBegin
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM product_images WHERE colour_id IS NULL) THEN
    RAISE EXCEPTION 'product_images rows left without a colour';
  END IF;
END $$;
-- +goose StatementEnd

ALTER TABLE product_images ALTER COLUMN colour_id SET NOT NULL;
CREATE INDEX product_images_colour_position_idx ON product_images (colour_id, position);
DROP INDEX product_images_product_position_idx;

-- product_id stays for one release, nullable, and the repository keeps
-- filling it: the release before this one reads photos by product_id, and
-- Fly migrates before the new release serves. If the deploy fails after
-- migrating, the old release still shows every photo. A later migration
-- drops it.
ALTER TABLE product_images ALTER COLUMN product_id DROP NOT NULL;

-- +goose Down

-- Lossy: colour names and every quantity are gone. Photos go back to
-- their piece, the colours' photos one after the other, in colour order.
UPDATE product_images i SET product_id = c.product_id
FROM product_colours c
WHERE c.id = i.colour_id AND i.product_id IS DISTINCT FROM c.product_id;

UPDATE product_images i SET position = r.position
FROM (
  SELECT i.id, row_number() OVER (PARTITION BY c.product_id ORDER BY c.position, i.position, i.id) - 1 AS position
  FROM product_images i JOIN product_colours c ON c.id = i.colour_id
) r
WHERE r.id = i.id AND i.position <> r.position;

ALTER TABLE product_images ALTER COLUMN product_id SET NOT NULL;
CREATE INDEX product_images_product_position_idx ON product_images (product_id, position);
DROP INDEX product_images_colour_position_idx;
ALTER TABLE product_images DROP COLUMN colour_id;

DROP TABLE product_variations;
DROP TABLE product_colours;
