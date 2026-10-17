-- +goose Up

-- Pending changes used to be "a product with updated_at after the last
-- publish", which misses two kinds of change: deleting a product leaves no
-- row behind to compare, and adding or removing a photo never touches the
-- product row. site_state now records when the catalogue last changed,
-- kept by triggers so no query can forget to do it.
ALTER TABLE site_state ADD COLUMN last_changed_at TIMESTAMPTZ;

UPDATE site_state SET last_changed_at = (SELECT max(updated_at) FROM products);

-- Row-level on purpose: a statement-level trigger also fires when the
-- statement matched nothing, and would show a change that never happened.
-- +goose StatementBegin
CREATE FUNCTION mark_site_changed() RETURNS TRIGGER AS $$
BEGIN
  UPDATE site_state SET last_changed_at = now();
  RETURN NULL;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER products_mark_site_changed
  AFTER INSERT OR UPDATE OR DELETE ON products
  FOR EACH ROW EXECUTE FUNCTION mark_site_changed();

CREATE TRIGGER product_images_mark_site_changed
  AFTER INSERT OR UPDATE OR DELETE ON product_images
  FOR EACH ROW EXECUTE FUNCTION mark_site_changed();

-- +goose Down
DROP TRIGGER product_images_mark_site_changed ON product_images;
DROP TRIGGER products_mark_site_changed ON products;
DROP FUNCTION mark_site_changed;
ALTER TABLE site_state DROP COLUMN last_changed_at;
