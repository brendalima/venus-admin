-- +goose Up

-- Archiving takes a piece out of the collection the site shows while
-- keeping everything it has: photos, files and all. NULL is the normal
-- state; the time answers when it left, which is what someone finding it
-- again will want to know. Deleting for good stays a separate, explicit
-- step.
ALTER TABLE products ADD COLUMN archived_at TIMESTAMPTZ;

-- What the daily list and /api/products read: the collection, in order.
CREATE INDEX products_collection_idx ON products (position) WHERE archived_at IS NULL;

-- +goose Down
-- Lossy: archived pieces come back into the collection, and onto the site
-- on the next publish.
DROP INDEX products_collection_idx;
ALTER TABLE products DROP COLUMN archived_at;
