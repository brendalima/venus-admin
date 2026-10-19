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

// PasswordResetService is "Esqueci minha senha": a link by email that lets
// someone set a new password without the developer.
type PasswordResetService interface {
	// Request emails a reset link when email belongs to an account, and
	// otherwise does nothing; either way it returns nil, so the caller
	// cannot tell whether the account exists. An error means the link
	// could not be created or sent.
	Request(ctx context.Context, email string) error

	// Check returns *domain.NotFoundError when the link's token is
	// unknown, already used or expired.
	Check(ctx context.Context, token string) error

	// Reset sets password for the token's user and uses the link up. It
	// returns the user, with the new password version, so the caller can
	// log them in; a bad password comes back as *domain.ValidationError,
	// and a dead link as *domain.NotFoundError.
	Reset(ctx context.Context, token, password string) (domain.User, error)
}

// ProductService manages the catalogue. A product's colours, with their
// photos and stock, are saved with it: Create and Update apply a colour
// edit together with the fields, all or nothing.
type ProductService interface {
	// List returns the collection, which is what the site shows: archived
	// products are left out.
	List(ctx context.Context) ([]domain.Product, error)
	ListArchived(ctx context.Context) ([]domain.Product, error)
	CountArchived(ctx context.Context) (int, error)

	// GetByID finds a product whether it is archived or not.
	GetByID(ctx context.Context, id string) (domain.Product, error)

	// Create derives the slug from the name, places the product at the
	// end of the catalogue, and gives it colours, which must hold at
	// least one. Invalid input comes back as joined
	// *domain.ValidationError, one per field and one per new photo that
	// is not usable (domain.ColourField with domain.PhotoErrorField), all
	// at once.
	Create(ctx context.Context, p domain.Product, colours domain.ColoursEdit) (domain.Product, error)

	// Update keeps the product's place in the catalogue. The slug only
	// changes when p.Slug asks for it; an empty one is derived from the
	// name again. A nil colours leaves the colours, photos and stock as
	// they are. Colours or photos that changed elsewhere since they were
	// loaded come back as *domain.StaleError, with nothing saved.
	Update(ctx context.Context, p domain.Product, colours *domain.ColoursEdit) (domain.Product, error)

	SetSoldOut(ctx context.Context, id string, soldOut bool) (domain.Product, error)

	// SetStock sets how many pieces of size the colour has, as the list's
	// quick buttons do: the quantity wanted, not a change, so a double tap
	// asks for the same thing twice. The size must be one of the
	// product's.
	SetStock(ctx context.Context, productID, colourID, size string, quantity int32) (domain.Product, error)

	// SetArchived takes the product out of the collection, keeping its
	// colours, photos and stock, or puts it back where it was.
	SetArchived(ctx context.Context, id string, archived bool) (domain.Product, error)

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
