package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/brendalima/venus-admin/internal/core/domain"
)

var _ ProductRepository = (*Product)(nil)

const (
	productColumns = `id, slug, name, price_cents, sizes, description, details, sold_out, position, created_at, updated_at`
	imageColumns   = `id, product_id, key_600, key_1200, position, created_at`

	pgUniqueViolation = "23505"
	productsSlugKey   = "products_slug_key"
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

func (r *Product) List(ctx context.Context) ([]domain.Product, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+productColumns+` FROM products ORDER BY position, created_at, id`)
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

	if err := r.attachImages(ctx, products); err != nil {
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

func (r *Product) Create(ctx context.Context, p domain.Product) (domain.Product, error) {
	// Taking max(position) + 1 can race with another insert, which at
	// worst leaves two products sharing a position; ordering then falls
	// back to created_at. With two or three people using the admin that
	// is not worth a lock.
	err := r.db.QueryRowContext(ctx,
		`INSERT INTO products (id, slug, name, price_cents, sizes, description, details, sold_out, position)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8,
		         (SELECT COALESCE(max(position), 0) + 1 FROM products))
		 RETURNING position, created_at, updated_at`,
		p.ID, p.Slug, p.Name, p.PriceCents, nonNil(p.Sizes), p.Description, nonNil(p.Details), p.SoldOut,
	).Scan(&p.Position, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return domain.Product{}, translateWriteError(err, "couldn't create product")
	}

	p.Images = []domain.Image{}
	return p, nil
}

func (r *Product) Update(ctx context.Context, p domain.Product) (domain.Product, error) {
	if uuid.Validate(p.ID) != nil {
		return domain.Product{}, domain.NewNotFoundError("product not found")
	}

	err := r.db.QueryRowContext(ctx,
		`UPDATE products
		 SET slug = $2, name = $3, price_cents = $4, sizes = $5, description = $6,
		     details = $7, sold_out = $8, position = $9
		 WHERE id = $1
		 RETURNING created_at, updated_at`,
		p.ID, p.Slug, p.Name, p.PriceCents, nonNil(p.Sizes), p.Description, nonNil(p.Details), p.SoldOut, p.Position,
	).Scan(&p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Product{}, domain.NewNotFoundError("product not found")
	}
	if err != nil {
		return domain.Product{}, translateWriteError(err, "couldn't update product")
	}

	// The caller's Images may be stale; return what is stored.
	updated := []domain.Product{p}
	if err := r.attachImages(ctx, updated); err != nil {
		return domain.Product{}, err
	}
	return updated[0], nil
}

// Delete removes the product and, by cascade, its image rows. The files in
// the bucket are not the repository's to remove: the caller must read the keys
// first if it wants them gone.
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
	if err := r.attachImages(ctx, found); err != nil {
		return domain.Product{}, err
	}
	return found[0], nil
}

// attachImages loads the images of every product in one query, instead of
// one per product.
func (r *Product) attachImages(ctx context.Context, products []domain.Product) error {
	if len(products) == 0 {
		return nil
	}

	ids := make([]string, len(products))
	byID := make(map[string]*domain.Product, len(products))
	for i := range products {
		products[i].Images = []domain.Image{}
		ids[i] = products[i].ID
		byID[products[i].ID] = &products[i]
	}

	rows, err := r.db.QueryContext(ctx,
		`SELECT `+imageColumns+` FROM product_images
		 WHERE product_id = ANY($1)
		 ORDER BY product_id, position, created_at, id`, ids)
	if err != nil {
		return fmt.Errorf("couldn't list product images: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var img domain.Image
		err := rows.Scan(&img.ID, &img.ProductID, &img.Key600, &img.Key1200, &img.Position, &img.CreatedAt)
		if err != nil {
			return fmt.Errorf("couldn't read product image: %w", err)
		}
		p := byID[img.ProductID]
		p.Images = append(p.Images, img)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("couldn't list product images: %w", err)
	}
	return nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanProduct(s scanner) (domain.Product, error) {
	var p domain.Product
	err := s.Scan(&p.ID, &p.Slug, &p.Name, &p.PriceCents, &p.Sizes, &p.Description,
		&p.Details, &p.SoldOut, &p.Position, &p.CreatedAt, &p.UpdatedAt)
	return p, err
}

func translateWriteError(err error, action string) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation && pgErr.ConstraintName == productsSlugKey {
		return domain.NewConflictError("slug already in use")
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
