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

// ProductService manages the catalogue. Photos are not handled here yet
// (M2): writes leave a product's images as they are.
type ProductService interface {
	List(ctx context.Context) ([]domain.Product, error)
	GetByID(ctx context.Context, id string) (domain.Product, error)

	// Create derives the slug from the name and places the product at the
	// end of the catalogue. Invalid input comes back as joined
	// *domain.ValidationError, one per field.
	Create(ctx context.Context, p domain.Product) (domain.Product, error)

	// Update keeps the product's place in the catalogue. The slug only
	// changes when p.Slug asks for it; an empty one is derived from the
	// name again.
	Update(ctx context.Context, p domain.Product) (domain.Product, error)

	SetSoldOut(ctx context.Context, id string, soldOut bool) (domain.Product, error)
	Delete(ctx context.Context, id string) error
}
