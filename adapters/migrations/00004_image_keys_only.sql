-- +goose Up

-- Photos are described by their position alone: the first is the cover,
-- and the owners arrange the rest. The closed list of angles (Frente,
-- Costas...) was more limiting than useful, so the label goes.
--
-- Only the object keys are stored. Public URLs are built from a base URL
-- in the configuration, so moving the bucket to another domain is a
-- config change instead of rewriting every row. The keys lose their r2_
-- prefix: the column says what it holds, not which provider holds it.
ALTER TABLE product_images
  DROP COLUMN label,
  DROP COLUMN url_600,
  DROP COLUMN url_1200;

ALTER TABLE product_images RENAME COLUMN r2_key_600 TO key_600;
ALTER TABLE product_images RENAME COLUMN r2_key_1200 TO key_1200;

-- +goose Down

-- The dropped values cannot be recovered; the columns come back empty.
ALTER TABLE product_images RENAME COLUMN key_1200 TO r2_key_1200;
ALTER TABLE product_images RENAME COLUMN key_600 TO r2_key_600;

ALTER TABLE product_images
  ADD COLUMN label    TEXT NOT NULL DEFAULT '',
  ADD COLUMN url_600  TEXT NOT NULL DEFAULT '',
  ADD COLUMN url_1200 TEXT NOT NULL DEFAULT '';
