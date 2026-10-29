-- +goose Up

-- Collections the pieces are grouped in ("Verão 2026"). Registered on
-- their own screen and chosen in the piece's form, so a name is typed
-- once and renamed in one place. IDs come from the services, as for
-- products.
CREATE TABLE collections (
  id         UUID PRIMARY KEY,
  name       TEXT NOT NULL CHECK (length(trim(name)) > 0),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- The repository turns a clash here into a message on the name.
CREATE UNIQUE INDEX collections_name_key ON collections (lower(name));

-- A piece belongs to one collection or to none, which is where every
-- piece before this starts. Deleting a collection leaves its pieces
-- without one: they are the owners' work, the collection only a label.
ALTER TABLE products
  ADD COLUMN collection_id UUID REFERENCES collections(id) ON DELETE SET NULL;
CREATE INDEX products_collection_id_idx ON products (collection_id);

-- Renaming changes what the site shows. Adding a collection does not,
-- until a piece joins it, and deleting one changes its pieces, whose own
-- trigger fires; so only updates count here.
CREATE TRIGGER collections_mark_site_changed
  AFTER UPDATE ON collections
  FOR EACH ROW EXECUTE FUNCTION mark_site_changed();

-- +goose Down
ALTER TABLE products DROP COLUMN collection_id;
DROP TABLE collections;
