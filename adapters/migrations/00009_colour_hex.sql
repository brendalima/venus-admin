-- +goose Up

-- The colour as codes, for the site to paint a swatch with: the main one
-- first, then the secondary ones of a print or a trim. Empty for every
-- colour before this, and a piece can go on sale without any. Stored
-- lowercase, as the browser's colour picker gives them, so the check
-- stays simple; how many there may be is the domain's call.
ALTER TABLE product_colours
  ADD COLUMN hex TEXT[] NOT NULL DEFAULT '{}'
  CHECK (cardinality(hex) = 0
         OR array_to_string(hex, ',', 'null') ~ '^#[0-9a-f]{6}(,#[0-9a-f]{6})*$');

-- +goose Down
ALTER TABLE product_colours DROP COLUMN hex;
