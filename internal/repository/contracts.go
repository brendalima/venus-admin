package repository

import (
	"context"

	"github.com/brendalima/venus-admin/internal/core/domain"
)

type HealthRepository interface {
	Ping(ctx context.Context) error
}

// ProductRepository persists products. Reads return each product with
// its images, cover first; writes leave the images alone, since photos
// have their own lifecycle (upload, resize, R2).
type ProductRepository interface {
	List(ctx context.Context) ([]domain.Product, error)
	GetByID(ctx context.Context, id string) (domain.Product, error)
	GetBySlug(ctx context.Context, slug string) (domain.Product, error)

	// Create places the new product at the end of the catalogue,
	// ignoring p.Position.
	Create(ctx context.Context, p domain.Product) (domain.Product, error)
	Update(ctx context.Context, p domain.Product) (domain.Product, error)
	Delete(ctx context.Context, id string) error

	// HasUnpublishedChanges reports whether the catalogue changed after
	// the last publish, which is what drives the "pending changes" badge.
	HasUnpublishedChanges(ctx context.Context) (bool, error)
	MarkPublished(ctx context.Context) error
}

// UserRepository persists the people allowed into the admin. Emails
// arrive already normalised by the service.
type UserRepository interface {
	Create(ctx context.Context, u domain.User) (domain.User, error)
	GetByID(ctx context.Context, id string) (domain.User, error)
	GetByEmail(ctx context.Context, email string) (domain.User, error)
}

// SessionRepository is the housekeeping side of the session store. The
// same type also implements scs.CtxStore, which cmd/api hands to the
// session manager; the services only ever need to tidy up.
type SessionRepository interface {
	// DeleteExpired removes sessions past their expiry and returns how
	// many went.
	DeleteExpired(ctx context.Context) (int64, error)
}
