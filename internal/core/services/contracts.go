package services

import (
	"context"

	"github.com/brendalima/venus-admin/internal/core/domain"
)

type HealthService interface {
	// Ready reports whether the dependencies needed to serve are reachable.
	Ready(ctx context.Context) error
}

type UserService interface {
	// Create adds a person to the admin. It is only reachable from
	// cmd/adduser: there is no public signup.
	Create(ctx context.Context, email, password string) (domain.User, error)

	// Authenticate returns the user when email and password match, and
	// *domain.UnauthorizedError when they do not, without saying which
	// of the two was wrong.
	Authenticate(ctx context.Context, email, password string) (domain.User, error)

	GetByID(ctx context.Context, id string) (domain.User, error)
}

// ProductService manages the catalogue. A product's photos are saved
// with it: Create and Update apply a photo edit together with the fields,
// all or nothing.
type ProductService interface {
	List(ctx context.Context) ([]domain.Product, error)
	GetByID(ctx context.Context, id string) (domain.Product, error)

	// Create derives the slug from the name, places the product at the
	// end of the catalogue, and adds the photos in photos, which may be
	// nil. Invalid input comes back as joined *domain.ValidationError,
	// one per field and one per new photo that is not usable
	// (domain.PhotoErrorField), all at once.
	Create(ctx context.Context, p domain.Product, photos *domain.PhotoEdit) (domain.Product, error)

	// Update keeps the product's place in the catalogue. The slug only
	// changes when p.Slug asks for it; an empty one is derived from the
	// name again. A nil photos leaves the photos as they are. A photo
	// list that changed elsewhere since it was loaded comes back as
	// *domain.StaleError, with nothing saved.
	Update(ctx context.Context, p domain.Product, photos *domain.PhotoEdit) (domain.Product, error)

	SetSoldOut(ctx context.Context, id string, soldOut bool) (domain.Product, error)

	// Delete removes the product and its photo files.
	Delete(ctx context.Context, id string) error
}

// SiteService publishes the catalogue to the public site, which is static
// and rebuilt from /api/products on each publish.
type SiteService interface {
	Status(ctx context.Context) (domain.SiteStatus, error)

	// Publish starts a rebuild of the site and records the catalogue as
	// published. It does not wait for the build: if the build fails, the
	// changes show as pending again on the next look, never as published.
	Publish(ctx context.Context) error
}
