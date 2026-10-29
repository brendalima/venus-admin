package repository

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/brendalima/venus-admin/internal/core/domain"
)

var (
	_ ProductRepository = (*Product)(nil)
	_ SiteRepository    = (*Product)(nil)
)

const (
	// collectionNameColumn reads the name of a product's collection, for
	// showing it; empty when the piece is in none.
	collectionNameColumn = `COALESCE((SELECT c.name FROM collections c WHERE c.id = products.collection_id), '')`
	productColumns       = `id, slug, name, price_cents, sizes, description, COALESCE(collection_id::text, ''), ` +
		collectionNameColumn + `, details, sold_out, position, archived_at, created_at, updated_at`

	pgUniqueViolation     = "23505"
	pgForeignKeyViolation = "23503"
	productsSlugKey       = "products_slug_key"
	coloursNameKey        = "product_colours_product_name_key"
	collectionsNameKey    = "collections_name_key"
)

type Product struct {
	db *sql.DB
}

func NewProduct(db *sql.DB) (*Product, error) {
	if db == nil {
		return nil, errors.New("missing database dependency")
	}
	return &Product{db: db}, nil
}

func MustNewProduct(db *sql.DB) *Product {
	p, err := NewProduct(db)
	if err != nil {
		panic(fmt.Errorf("couldn't create product repository: %w", err))
	}
	return p
}

// List returns the collection: every product not archived, in catalogue
// order. Anything that shows products to the site goes through here, so
// an archived piece cannot reappear by a query forgetting to filter.
func (r *Product) List(ctx context.Context) ([]domain.Product, error) {
	return r.list(ctx,
		`SELECT `+productColumns+` FROM products WHERE archived_at IS NULL ORDER BY position, created_at, id`)
}

// ListArchived returns the archived products, the most recently archived
// first.
func (r *Product) ListArchived(ctx context.Context) ([]domain.Product, error) {
	return r.list(ctx,
		`SELECT `+productColumns+` FROM products WHERE archived_at IS NOT NULL ORDER BY archived_at DESC, id`)
}

func (r *Product) CountArchived(ctx context.Context) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM products WHERE archived_at IS NOT NULL`).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("couldn't count archived products: %w", err)
	}
	return n, nil
}

func (r *Product) list(ctx context.Context, query string) ([]domain.Product, error) {
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("couldn't list products: %w", err)
	}
	defer rows.Close()

	products := []domain.Product{}
	for rows.Next() {
		p, err := scanProduct(rows)
		if err != nil {
			return nil, fmt.Errorf("couldn't read product: %w", err)
		}
		products = append(products, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("couldn't list products: %w", err)
	}

	if err := attachColours(ctx, r.db, products); err != nil {
		return nil, err
	}
	return products, nil
}

func (r *Product) GetByID(ctx context.Context, id string) (domain.Product, error) {
	// An ID that is not a UUID cannot exist; asking Postgres would only
	// turn a typo in the URL into a 500.
	if uuid.Validate(id) != nil {
		return domain.Product{}, domain.NewNotFoundError("product not found")
	}
	return r.getOne(ctx, `SELECT `+productColumns+` FROM products WHERE id = $1`, id)
}

func (r *Product) GetBySlug(ctx context.Context, slug string) (domain.Product, error) {
	return r.getOne(ctx, `SELECT `+productColumns+` FROM products WHERE slug = $1`, slug)
}

// Create inserts the product and its images, in the order given, in one
// transaction: a product never exists without the photos it was saved
// with.
// Create inserts the product with its colours, their photos and stock, in
// the order given, in one transaction: a product never exists without what
// it was saved with.
func (r *Product) Create(ctx context.Context, p domain.Product) (domain.Product, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Product{}, fmt.Errorf("couldn't begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Taking max(position) + 1 can race with another insert, which at
	// worst leaves two products sharing a position; ordering then falls
	// back to created_at. With two or three people using the admin that
	// is not worth a lock.
	err = tx.QueryRowContext(ctx,
		`INSERT INTO products (id, slug, name, price_cents, sizes, description, collection_id, details, sold_out, position)
		 VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, '')::uuid, $8, $9,
		         (SELECT COALESCE(max(position), 0) + 1 FROM products))
		 RETURNING position, created_at, updated_at, `+collectionNameColumn,
		p.ID, p.Slug, p.Name, p.PriceCents, nonNil(p.Sizes), p.Description, p.CollectionID, nonNil(p.Details), p.SoldOut,
	).Scan(&p.Position, &p.CreatedAt, &p.UpdatedAt, &p.CollectionName)
	if err != nil {
		return domain.Product{}, translateWriteError(err, "couldn't create product")
	}

	for i, c := range p.Colours {
		if err := insertColour(ctx, tx, p.ID, c, i); err != nil {
			return domain.Product{}, err
		}
	}

	created := []domain.Product{p}
	if err := attachColours(ctx, tx, created); err != nil {
		return domain.Product{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Product{}, fmt.Errorf("couldn't commit product: %w", err)
	}
	return created[0], nil
}

func (r *Product) Update(ctx context.Context, p domain.Product) (domain.Product, error) {
	if uuid.Validate(p.ID) != nil {
		return domain.Product{}, domain.NewNotFoundError("product not found")
	}
	p, err := updateFields(ctx, r.db, p)
	if err != nil {
		return domain.Product{}, err
	}

	// The caller's Colours may be stale; return what is stored.
	updated := []domain.Product{p}
	if err := attachColours(ctx, r.db, updated); err != nil {
		return domain.Product{}, err
	}
	return updated[0], nil
}

func (r *Product) UpdateWithColours(ctx context.Context, p domain.Product, changes domain.ColourChanges) (domain.Product, []domain.Image, error) {
	if uuid.Validate(p.ID) != nil {
		return domain.Product{}, nil, domain.NewNotFoundError("product not found")
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Product{}, nil, fmt.Errorf("couldn't begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Updating the product's row first also locks it, so a concurrent
	// save of the same product waits and then sees this one's result
	// rather than interleaving with it.
	if p, err = updateFields(ctx, tx, p); err != nil {
		return domain.Product{}, nil, err
	}
	current, err := lockColours(ctx, tx, p.ID)
	if err != nil {
		return domain.Product{}, nil, err
	}

	gone, err := checkColourChanges(p.Colours, current, changes)
	if err != nil {
		return domain.Product{}, nil, err
	}
	removedImages := set(changes.RemovedImages)
	var goneFromKept []string
	for _, img := range gone {
		if removedImages[img.ID] {
			goneFromKept = append(goneFromKept, img.ID)
		}
	}

	// Removed photos and colours go first, freeing their names for the
	// colours that follow. Deleting a colour takes its photos and stock by
	// cascade; gone already holds those photos, read before they went.
	if _, err := tx.ExecContext(ctx, `DELETE FROM product_images WHERE id = ANY($1)`, nonNil(goneFromKept)); err != nil {
		return domain.Product{}, nil, fmt.Errorf("couldn't delete images: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM product_colours WHERE id = ANY($1)`, nonNil(changes.RemovedColours)); err != nil {
		return domain.Product{}, nil, fmt.Errorf("couldn't delete colours: %w", err)
	}

	// The unique index on names is checked row by row, so swapping two
	// colours' names would clash halfway. Renamed colours step aside
	// under their own ID first.
	var renamed []string
	for _, c := range p.Colours {
		if stored, ok := current[c.ID]; ok && stored.Name != c.Name {
			renamed = append(renamed, c.ID)
		}
	}
	if len(renamed) > 0 {
		if _, err := tx.ExecContext(ctx,
			`UPDATE product_colours SET name = id::text WHERE id = ANY($1)`, renamed,
		); err != nil {
			return domain.Product{}, nil, fmt.Errorf("couldn't rename colours: %w", err)
		}
	}

	for i, c := range p.Colours {
		stored, exists := current[c.ID]
		if !exists {
			if err := insertColour(ctx, tx, p.ID, c, i); err != nil {
				return domain.Product{}, nil, err
			}
			continue
		}
		if err := updateColour(ctx, tx, p.ID, stored, c, i); err != nil {
			return domain.Product{}, nil, err
		}
	}

	updated := []domain.Product{p}
	if err := attachColours(ctx, tx, updated); err != nil {
		return domain.Product{}, nil, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Product{}, nil, fmt.Errorf("couldn't commit product: %w", err)
	}
	return updated[0], gone, nil
}

// checkColourChanges compares the colours and photos wanted with the ones
// stored, which changes says how to reconcile, and returns the photos that
// will be gone: the ones removed, and every photo of a removed colour.
// Anything wanted that is neither stored nor added, or stored and neither
// wanted nor removed, means the product changed since it was read.
func checkColourChanges(wanted []domain.Colour, current map[string]domain.Colour, changes domain.ColourChanges) ([]domain.Image, error) {
	stale := domain.NewStaleError("colours or photos changed since they were loaded")
	addedColours, removedColours := set(changes.AddedColours), set(changes.RemovedColours)
	addedImages, removedImages := set(changes.AddedImages), set(changes.RemovedImages)

	var gone []domain.Image
	kept := map[string]bool{}
	for _, c := range wanted {
		kept[c.ID] = true
		stored, exists := current[c.ID]
		// A colour kept on screen but deleted meanwhile cannot be kept:
		// its photos' files are gone.
		if !exists && !addedColours[c.ID] {
			return nil, stale
		}

		storedImages := map[string]bool{}
		for _, img := range stored.Images {
			storedImages[img.ID] = true
		}
		keptImages := map[string]bool{}
		for _, img := range c.Images {
			keptImages[img.ID] = true
			if !storedImages[img.ID] && !addedImages[img.ID] {
				return nil, stale
			}
		}
		for _, img := range stored.Images {
			switch {
			case keptImages[img.ID]:
			case removedImages[img.ID]:
				gone = append(gone, img)
			default: // added elsewhere, never shown to this person
				return nil, stale
			}
		}
	}
	for id, c := range current {
		switch {
		case kept[id]:
		case removedColours[id]:
			gone = append(gone, c.Images...)
		default:
			return nil, stale
		}
	}
	return gone, nil
}

// SetArchived takes the product out of the collection, or puts it back,
// leaving everything else as it is. Its place in the catalogue is kept,
// so a piece put back returns where it was.
func (r *Product) SetArchived(ctx context.Context, id string, archived bool) (domain.Product, error) {
	if uuid.Validate(id) != nil {
		return domain.Product{}, domain.NewNotFoundError("product not found")
	}
	p, err := scanProduct(r.db.QueryRowContext(ctx,
		`UPDATE products
		 SET archived_at = CASE WHEN $2 THEN now() END
		 WHERE id = $1
		 RETURNING `+productColumns,
		id, archived,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Product{}, domain.NewNotFoundError("product not found")
	}
	if err != nil {
		return domain.Product{}, fmt.Errorf("couldn't archive product: %w", err)
	}

	updated := []domain.Product{p}
	if err := attachColours(ctx, r.db, updated); err != nil {
		return domain.Product{}, err
	}
	return updated[0], nil
}

// SetStock sets how many pieces of size a colour has, counting it from now
// on if it was not counted.
func (r *Product) SetStock(ctx context.Context, colourID, size string, quantity int32) error {
	if uuid.Validate(colourID) != nil {
		return domain.NewNotFoundError("colour not found")
	}
	err := upsertStock(ctx, r.db, colourID, domain.Stock{Size: size, Quantity: quantity})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgForeignKeyViolation {
		return domain.NewNotFoundError("colour not found")
	}
	return err
}

// updateFields saves the product's own fields, never archived_at, and
// fills in what the database keeps.
func updateFields(ctx context.Context, q querier, p domain.Product) (domain.Product, error) {
	err := q.QueryRowContext(ctx,
		`UPDATE products
		 SET slug = $2, name = $3, price_cents = $4, sizes = $5, description = $6,
		     collection_id = NULLIF($7, '')::uuid, details = $8, sold_out = $9, position = $10
		 WHERE id = $1
		 RETURNING archived_at, created_at, updated_at, `+collectionNameColumn,
		p.ID, p.Slug, p.Name, p.PriceCents, nonNil(p.Sizes), p.Description, p.CollectionID, nonNil(p.Details), p.SoldOut, p.Position,
	).Scan((*nullTime)(&p.ArchivedAt), &p.CreatedAt, &p.UpdatedAt, &p.CollectionName)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Product{}, domain.NewNotFoundError("product not found")
	}
	if err != nil {
		return domain.Product{}, translateWriteError(err, "couldn't update product")
	}
	return p, nil
}

// lockColours reads a product's colours, with their photos and stock, by
// ID, holding the rows until the transaction ends.
func lockColours(ctx context.Context, tx *sql.Tx, productID string) (map[string]domain.Colour, error) {
	colours, err := readColours(ctx, tx, []string{productID}, true)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]domain.Colour, len(colours))
	for _, c := range colours {
		byID[c.ID] = c
	}
	return byID, nil
}

func insertColour(ctx context.Context, tx *sql.Tx, productID string, c domain.Colour, position int) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO product_colours (id, product_id, name, hex, position) VALUES ($1, $2, $3, $4, $5)`,
		c.ID, productID, c.Name, nonNil(c.Hexes), position)
	if err != nil {
		return translateWriteError(err, "couldn't add colour")
	}
	for i, img := range c.Images {
		if err := insertImage(ctx, tx, productID, c.ID, img, i); err != nil {
			return err
		}
	}
	for _, s := range c.Stock {
		if err := upsertStock(ctx, tx, c.ID, s); err != nil {
			return err
		}
	}
	return nil
}

// updateColour makes the stored colour what c says. Only what really
// changed is written, so the change triggers fire for that alone. Photos
// in c that stored does not have are new; the removed ones are already
// gone.
func updateColour(ctx context.Context, tx *sql.Tx, productID string, stored, c domain.Colour, position int) error {
	if stored.Name != c.Name || !slices.Equal(stored.Hexes, c.Hexes) || int(stored.Position) != position {
		if _, err := tx.ExecContext(ctx,
			`UPDATE product_colours SET name = $2, hex = $3, position = $4 WHERE id = $1`,
			c.ID, c.Name, nonNil(c.Hexes), position,
		); err != nil {
			return translateWriteError(err, "couldn't update colour")
		}
	}

	storedImages := make(map[string]domain.Image, len(stored.Images))
	for _, img := range stored.Images {
		storedImages[img.ID] = img
	}
	for i, img := range c.Images {
		was, exists := storedImages[img.ID]
		if !exists {
			if err := insertImage(ctx, tx, productID, c.ID, img, i); err != nil {
				return err
			}
			continue
		}
		if int(was.Position) != i {
			if _, err := tx.ExecContext(ctx,
				`UPDATE product_images SET position = $2 WHERE id = $1`, img.ID, i,
			); err != nil {
				return fmt.Errorf("couldn't reorder image: %w", err)
			}
		}
	}

	storedStock := make(map[string]int32, len(stored.Stock))
	for _, s := range stored.Stock {
		storedStock[s.Size] = s.Quantity
	}
	for _, s := range c.Stock {
		if q, ok := storedStock[s.Size]; ok && q == s.Quantity {
			delete(storedStock, s.Size)
			continue
		}
		delete(storedStock, s.Size)
		if err := upsertStock(ctx, tx, c.ID, s); err != nil {
			return err
		}
	}
	for size := range storedStock { // no longer counted
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM product_variations WHERE colour_id = $1 AND size = $2`, c.ID, size,
		); err != nil {
			return fmt.Errorf("couldn't remove stock: %w", err)
		}
	}
	return nil
}

// insertImage writes product_id as well as colour_id: the release before
// colours reads photos by product_id, and migration 00008 keeps it filled
// for as long as a rollback to that release is possible.
func insertImage(ctx context.Context, tx *sql.Tx, productID, colourID string, img domain.Image, position int) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO product_images (id, product_id, colour_id, key_600, key_1200, position)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		img.ID, productID, colourID, img.Key600, img.Key1200, position)
	if err != nil {
		return fmt.Errorf("couldn't add image: %w", err)
	}
	return nil
}

func upsertStock(ctx context.Context, q querier, colourID string, s domain.Stock) error {
	_, err := q.ExecContext(ctx,
		`INSERT INTO product_variations (colour_id, size, quantity) VALUES ($1, $2, $3)
		 ON CONFLICT (colour_id, size) DO UPDATE SET quantity = EXCLUDED.quantity`,
		colourID, s.Size, s.Quantity)
	if err != nil {
		return fmt.Errorf("couldn't set stock: %w", err)
	}
	return nil
}

func set(ids []string) map[string]bool {
	m := make(map[string]bool, len(ids))
	for _, id := range ids {
		m[id] = true
	}
	return m
}

// Delete removes the product and, by cascade, its colours, image rows and
// stock. The files in the bucket are not the repository's to remove: the
// caller must read the keys first if it wants them gone.
func (r *Product) Delete(ctx context.Context, id string) error {
	if uuid.Validate(id) != nil {
		return domain.NewNotFoundError("product not found")
	}

	res, err := r.db.ExecContext(ctx, `DELETE FROM products WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("couldn't delete product: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("couldn't delete product: %w", err)
	}
	if n == 0 {
		return domain.NewNotFoundError("product not found")
	}
	return nil
}

// HasUnpublishedChanges compares the two timestamps in site_state, which
// triggers keep current on every product and image change (migration
// 00003). A catalogue that never changed has nothing to publish.
func (r *Product) HasUnpublishedChanges(ctx context.Context) (bool, error) {
	var pending bool
	err := r.db.QueryRowContext(ctx,
		`SELECT last_changed_at IS NOT NULL
		        AND (last_published_at IS NULL OR last_changed_at > last_published_at)
		 FROM site_state`,
	).Scan(&pending)
	if err != nil {
		return false, fmt.Errorf("couldn't read site state: %w", err)
	}
	return pending, nil
}

func (r *Product) MarkPublished(ctx context.Context) error {
	if _, err := r.db.ExecContext(ctx, `UPDATE site_state SET last_published_at = now()`); err != nil {
		return fmt.Errorf("couldn't mark site as published: %w", err)
	}
	return nil
}

func (r *Product) getOne(ctx context.Context, query string, arg any) (domain.Product, error) {
	p, err := scanProduct(r.db.QueryRowContext(ctx, query, arg))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Product{}, domain.NewNotFoundError("product not found")
	}
	if err != nil {
		return domain.Product{}, fmt.Errorf("couldn't get product: %w", err)
	}

	found := []domain.Product{p}
	if err := attachColours(ctx, r.db, found); err != nil {
		return domain.Product{}, err
	}
	return found[0], nil
}

// querier is what *sql.DB and *sql.Tx have in common, for code that runs
// in a transaction or outside one.
type querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// attachColours loads the colours of every product, with their photos and
// stock, in three queries however many products there are.
func attachColours(ctx context.Context, q querier, products []domain.Product) error {
	if len(products) == 0 {
		return nil
	}

	ids := make([]string, len(products))
	byID := make(map[string]*domain.Product, len(products))
	for i := range products {
		products[i].Colours = []domain.Colour{}
		ids[i] = products[i].ID
		byID[products[i].ID] = &products[i]
	}

	colours, err := readColours(ctx, q, ids, false)
	if err != nil {
		return err
	}
	for _, c := range colours {
		p := byID[c.ProductID]
		// Stock follows the product's sizes, the order the form and the
		// site show them in.
		slices.SortStableFunc(c.Stock, func(a, b domain.Stock) int {
			return cmp.Compare(sizeRank(p.Sizes, a.Size), sizeRank(p.Sizes, b.Size))
		})
		p.Colours = append(p.Colours, c)
	}
	return nil
}

func sizeRank(sizes []string, size string) int {
	if i := slices.Index(sizes, size); i >= 0 {
		return i
	}
	return len(sizes)
}

// Image and stock rows are reached through their colour, so the lock on
// them names the table: FOR UPDATE alone would lock the colours too, which
// is harmless but says more than meant.
const (
	readColoursQuery = `SELECT id, product_id, name, hex, position, created_at FROM product_colours
		WHERE product_id = ANY($1)
		ORDER BY product_id, position, created_at, id`
	readImagesQuery = `SELECT i.id, i.colour_id, i.key_600, i.key_1200, i.position, i.created_at
		FROM product_images i JOIN product_colours c ON c.id = i.colour_id
		WHERE c.product_id = ANY($1)
		ORDER BY i.colour_id, i.position, i.created_at, i.id`
	readStockQuery = `SELECT v.colour_id, v.size, v.quantity
		FROM product_variations v JOIN product_colours c ON c.id = v.colour_id
		WHERE c.product_id = ANY($1)`
)

// readColours returns the colours of the products in productIDs, in
// order, each with its photos and stock. With forUpdate, the rows stay
// locked until the transaction ends.
func readColours(ctx context.Context, q querier, productIDs []string, forUpdate bool) ([]domain.Colour, error) {
	coloursQuery, imagesQuery, stockQuery := readColoursQuery, readImagesQuery, readStockQuery
	if forUpdate {
		coloursQuery += ` FOR UPDATE`
		imagesQuery += ` FOR UPDATE OF i`
		stockQuery += ` FOR UPDATE OF v`
	}

	var colours []domain.Colour
	err := queryRows(ctx, q, coloursQuery, productIDs, func(rows *sql.Rows) error {
		c := domain.Colour{Images: []domain.Image{}, Stock: []domain.Stock{}}
		if err := rows.Scan(&c.ID, &c.ProductID, &c.Name, &c.Hexes, &c.Position, &c.CreatedAt); err != nil {
			return err
		}
		colours = append(colours, c)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("couldn't list colours: %w", err)
	}
	byID := make(map[string]*domain.Colour, len(colours))
	for i := range colours {
		byID[colours[i].ID] = &colours[i]
	}

	err = queryRows(ctx, q, imagesQuery, productIDs, func(rows *sql.Rows) error {
		var img domain.Image
		if err := rows.Scan(&img.ID, &img.ColourID, &img.Key600, &img.Key1200, &img.Position, &img.CreatedAt); err != nil {
			return err
		}
		// A colour added between the two queries is not in byID; its
		// photos are left for the next read, as the colour is.
		if c, ok := byID[img.ColourID]; ok {
			c.Images = append(c.Images, img)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("couldn't list product images: %w", err)
	}

	err = queryRows(ctx, q, stockQuery, productIDs, func(rows *sql.Rows) error {
		var colourID string
		var s domain.Stock
		if err := rows.Scan(&colourID, &s.Size, &s.Quantity); err != nil {
			return err
		}
		if c, ok := byID[colourID]; ok {
			c.Stock = append(c.Stock, s)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("couldn't list stock: %w", err)
	}
	return colours, nil
}

// queryRows runs query with arg and hands each row to scan.
func queryRows(ctx context.Context, q querier, query string, arg any, scan func(*sql.Rows) error) error {
	rows, err := q.QueryContext(ctx, query, arg)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := scan(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanProduct(s scanner) (domain.Product, error) {
	var p domain.Product
	err := s.Scan(&p.ID, &p.Slug, &p.Name, &p.PriceCents, &p.Sizes, &p.Description, &p.CollectionID, &p.CollectionName,
		&p.Details, &p.SoldOut, &p.Position, (*nullTime)(&p.ArchivedAt), &p.CreatedAt, &p.UpdatedAt)
	return p, err
}

// nullTime scans a nullable timestamp into a time.Time, NULL becoming the
// zero time, which is how the domain says "not set".
type nullTime time.Time

func (t *nullTime) Scan(src any) error {
	var n sql.NullTime
	if err := n.Scan(src); err != nil {
		return err
	}
	*t = nullTime(n.Time)
	return nil
}

func translateWriteError(err error, action string) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
		switch pgErr.ConstraintName {
		case productsSlugKey:
			return domain.NewConflictError(domain.ConflictSlug)
		case coloursNameKey:
			return domain.NewConflictError(domain.ConflictColourName)
		case collectionsNameKey:
			return domain.NewConflictError(domain.ConflictCollection)
		}
	}

	return fmt.Errorf("%s: %w", action, err)
}

// nonNil keeps an empty list from reaching the NOT NULL array columns as
// NULL, which is how a nil slice is encoded.
func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
