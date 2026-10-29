package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/brendalima/venus-admin/internal/core/domain"
)

var _ CollectionRepository = (*Collection)(nil)

const collectionColumns = `c.id, c.name, (SELECT count(*) FROM products p WHERE p.collection_id = c.id), c.created_at`

type Collection struct {
	db *sql.DB
}

func NewCollection(db *sql.DB) (*Collection, error) {
	if db == nil {
		return nil, errors.New("missing database dependency")
	}
	return &Collection{db: db}, nil
}

func MustNewCollection(db *sql.DB) *Collection {
	r, err := NewCollection(db)
	if err != nil {
		panic(fmt.Errorf("couldn't create collection repository: %w", err))
	}
	return r
}

func (r *Collection) List(ctx context.Context) ([]domain.Collection, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+collectionColumns+` FROM collections c ORDER BY c.created_at DESC, c.id`)
	if err != nil {
		return nil, fmt.Errorf("couldn't list collections: %w", err)
	}
	defer rows.Close()

	collections := []domain.Collection{}
	for rows.Next() {
		c, err := scanCollection(rows)
		if err != nil {
			return nil, fmt.Errorf("couldn't read collection: %w", err)
		}
		collections = append(collections, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("couldn't list collections: %w", err)
	}
	return collections, nil
}

func (r *Collection) GetByID(ctx context.Context, id string) (domain.Collection, error) {
	if uuid.Validate(id) != nil {
		return domain.Collection{}, domain.NewNotFoundError("collection not found")
	}
	c, err := scanCollection(r.db.QueryRowContext(ctx,
		`SELECT `+collectionColumns+` FROM collections c WHERE c.id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Collection{}, domain.NewNotFoundError("collection not found")
	}
	if err != nil {
		return domain.Collection{}, fmt.Errorf("couldn't get collection: %w", err)
	}
	return c, nil
}

func (r *Collection) Create(ctx context.Context, c domain.Collection) (domain.Collection, error) {
	err := r.db.QueryRowContext(ctx,
		`INSERT INTO collections (id, name) VALUES ($1, $2) RETURNING created_at`, c.ID, c.Name,
	).Scan(&c.CreatedAt)
	if err != nil {
		return domain.Collection{}, translateWriteError(err, "couldn't create collection")
	}
	c.Pieces = 0
	return c, nil
}

func (r *Collection) Rename(ctx context.Context, id, name string) (domain.Collection, error) {
	if uuid.Validate(id) != nil {
		return domain.Collection{}, domain.NewNotFoundError("collection not found")
	}
	// The same name is not written, so the publish badge does not light
	// for nothing. Whether the collection exists is the read's to say.
	_, err := r.db.ExecContext(ctx, `UPDATE collections SET name = $2 WHERE id = $1 AND name <> $2`, id, name)
	if err != nil {
		return domain.Collection{}, translateWriteError(err, "couldn't rename collection")
	}
	return r.GetByID(ctx, id)
}

func (r *Collection) Delete(ctx context.Context, id string) error {
	if uuid.Validate(id) != nil {
		return domain.NewNotFoundError("collection not found")
	}
	res, err := r.db.ExecContext(ctx, `DELETE FROM collections WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("couldn't delete collection: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("couldn't delete collection: %w", err)
	}
	if n == 0 {
		return domain.NewNotFoundError("collection not found")
	}
	return nil
}

func scanCollection(s scanner) (domain.Collection, error) {
	var c domain.Collection
	err := s.Scan(&c.ID, &c.Name, &c.Pieces, &c.CreatedAt)
	return c, err
}
