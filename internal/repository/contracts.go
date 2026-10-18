package repository

import (
	"context"

	"github.com/brendalima/venus-admin/internal/core/domain"
)

type HealthRepository interface {
	Ping(ctx context.Context) error
}

// ProductRepository persists products. Reads return each product with
// its images, cover first. Image files are not the repository's: it keeps
// their keys, and the caller stores and deletes the files.
type ProductRepository interface {
	List(ctx context.Context) ([]domain.Product, error)
	GetByID(ctx context.Context, id string) (domain.Product, error)
	GetBySlug(ctx context.Context, slug string) (domain.Product, error)

	// Create places the new product at the end of the catalogue,
	// ignoring p.Position, with p.Images in the order given.
	Create(ctx context.Context, p domain.Product) (domain.Product, error)

	// Update saves the product's fields and leaves its images alone.
	Update(ctx context.Context, p domain.Product) (domain.Product, error)

	// UpdateWithImages saves the fields and makes the product's images
	// exactly p.Images, in that order, in one transaction. The images
	// named in added are inserted; the ones in removed are deleted and
	// returned, so the caller can delete their files. Any other image
	// must be one the product has, and every image it has must be in
	// p.Images or removed. Otherwise the list changed since it was read:
	// nothing is saved and *domain.StaleError comes back.
	UpdateWithImages(ctx context.Context, p domain.Product, added, removed []string) (domain.Product, []domain.Image, error)
	Delete(ctx context.Context, id string) error
}

// SiteRepository records what the public site was last built from. The
// product repository implements it: the state lives next to the products
// and their triggers keep it current.
type SiteRepository interface {
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
