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

var _ ImageRepository = (*Image)(nil)

const pgForeignKeyViolation = "23503"

type Image struct {
	db *sql.DB
}

func NewImage(db *sql.DB) (*Image, error) {
	if db == nil {
		return nil, errors.New("missing database dependency")
	}
	return &Image{db: db}, nil
}

func MustNewImage(db *sql.DB) *Image {
	i, err := NewImage(db)
	if err != nil {
		panic(fmt.Errorf("couldn't create image repository: %w", err))
	}
	return i
}

func (r *Image) Add(ctx context.Context, img domain.Image) (domain.Image, error) {
	if uuid.Validate(img.ProductID) != nil {
		return domain.Image{}, domain.NewNotFoundError("product not found")
	}

	// Two uploads at the same moment could compute the same position;
	// reads order by created_at and id after position, so both still
	// show, in a stable order.
	err := r.db.QueryRowContext(ctx,
		`INSERT INTO product_images (id, product_id, key_600, key_1200, position)
		 VALUES ($1, $2, $3, $4,
		   (SELECT COALESCE(max(position) + 1, 0) FROM product_images WHERE product_id = $2))
		 RETURNING position, created_at`,
		img.ID, img.ProductID, img.Key600, img.Key1200,
	).Scan(&img.Position, &img.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgForeignKeyViolation {
			return domain.Image{}, domain.NewNotFoundError("product not found")
		}
		return domain.Image{}, fmt.Errorf("couldn't add image: %w", err)
	}
	return img, nil
}

func (r *Image) Delete(ctx context.Context, productID, imageID string) (domain.Image, error) {
	if uuid.Validate(productID) != nil || uuid.Validate(imageID) != nil {
		return domain.Image{}, domain.NewNotFoundError("image not found")
	}

	var img domain.Image
	err := r.db.QueryRowContext(ctx,
		`DELETE FROM product_images WHERE id = $1 AND product_id = $2 RETURNING `+imageColumns,
		imageID, productID,
	).Scan(&img.ID, &img.ProductID, &img.Key600, &img.Key1200, &img.Position, &img.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Image{}, domain.NewNotFoundError("image not found")
	}
	if err != nil {
		return domain.Image{}, fmt.Errorf("couldn't delete image: %w", err)
	}
	return img, nil
}

func (r *Image) Move(ctx context.Context, productID, imageID string, index int) error {
	if uuid.Validate(productID) != nil || uuid.Validate(imageID) != nil {
		return domain.NewNotFoundError("image not found")
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("couldn't begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Locked, so two people reordering at once take turns instead of
	// interleaving their writes.
	rows, err := tx.QueryContext(ctx,
		`SELECT id FROM product_images WHERE product_id = $1
		 ORDER BY position, created_at, id
		 FOR UPDATE`, productID)
	if err != nil {
		return fmt.Errorf("couldn't list images: %w", err)
	}
	defer rows.Close()

	var ids []string
	from := -1
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("couldn't read image: %w", err)
		}
		if id == imageID {
			from = len(ids)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("couldn't list images: %w", err)
	}
	if from < 0 {
		return domain.NewNotFoundError("image not found")
	}

	to := min(max(index, 0), len(ids)-1)
	if to == from {
		return nil
	}
	moved := ids[from]
	ids = append(ids[:from], ids[from+1:]...)
	ids = append(ids[:to], append([]string{moved}, ids[to:]...)...)

	// Every position is rewritten, which also closes gaps left by
	// deleted photos. Only rows whose position changes are touched, so
	// the change triggers fire for what really moved.
	if _, err := tx.ExecContext(ctx,
		`UPDATE product_images AS i SET position = o.ord - 1
		 FROM unnest($1::uuid[]) WITH ORDINALITY AS o(id, ord)
		 WHERE i.id = o.id AND i.position <> o.ord - 1`,
		ids,
	); err != nil {
		return fmt.Errorf("couldn't reorder images: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("couldn't commit image order: %w", err)
	}
	return nil
}
