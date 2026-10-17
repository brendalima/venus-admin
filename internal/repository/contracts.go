package repository

import (
	"context"

	"github.com/brendalima/venus-admin/internal/core/domain"
)

type HealthRepository interface {
	Ping(ctx context.Context) error
}

// ProductRepository persists products. Declared ahead of its
// implementation: it describes the shape of the next milestone.
type ProductRepository interface {
	List(ctx context.Context) ([]domain.Product, error)
	GetByID(ctx context.Context, id int64) (domain.Product, error)
	GetBySlug(ctx context.Context, slug string) (domain.Product, error)
	Create(ctx context.Context, p domain.Product) (domain.Product, error)
	Update(ctx context.Context, p domain.Product) (domain.Product, error)
	Delete(ctx context.Context, id int64) error

	// HasUnpublishedChanges reports whether any product changed after the
	// last publish, which is what drives the "pending changes" badge.
	HasUnpublishedChanges(ctx context.Context) (bool, error)
	MarkPublished(ctx context.Context) error
}
