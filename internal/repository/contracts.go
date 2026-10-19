package repository

import (
	"context"
	"time"

	"github.com/brendalima/venus-admin/internal/core/domain"
)

type HealthRepository interface {
	Ping(ctx context.Context) error
}

// ProductRepository persists products. Reads return each product with
// its colours, in order, each with its images, cover first, and its
// stock. Image files are not the repository's: it keeps their keys, and
// the caller stores and deletes the files.
type ProductRepository interface {
	// List returns the collection: only products not archived. It is
	// what the site is built from.
	List(ctx context.Context) ([]domain.Product, error)
	// ListArchived returns only the archived ones, most recent first.
	ListArchived(ctx context.Context) ([]domain.Product, error)
	CountArchived(ctx context.Context) (int, error)

	// GetByID and GetBySlug find a product archived or not.
	GetByID(ctx context.Context, id string) (domain.Product, error)
	GetBySlug(ctx context.Context, slug string) (domain.Product, error)

	// Create places the new product at the end of the catalogue,
	// ignoring p.Position, with p.Colours, their images and stock in the
	// order given. A colour name used twice in the product comes back as
	// *domain.ConflictError.
	Create(ctx context.Context, p domain.Product) (domain.Product, error)

	// Update saves the product's fields and leaves its colours alone.
	Update(ctx context.Context, p domain.Product) (domain.Product, error)

	// UpdateWithColours saves the fields and makes the product's colours
	// exactly p.Colours, in that order, each with exactly its images,
	// in order, and its stock, in one transaction. The colours and images
	// changes names as added are inserted; the ones it names as removed
	// are deleted, and every image that goes, removed colours' included,
	// is returned, so the caller can delete the files. Any other colour
	// or image must be one the product has, in that colour, and every one
	// it has must be kept or removed. Otherwise the product changed since
	// it was read: nothing is saved and *domain.StaleError comes back.
	UpdateWithColours(ctx context.Context, p domain.Product, changes domain.ColourChanges) (domain.Product, []domain.Image, error)

	// SetStock sets the quantity of one size of a colour, and
	// *domain.NotFoundError when the colour does not exist.
	SetStock(ctx context.Context, colourID, size string, quantity int32) error

	// SetArchived takes the product out of the collection, or puts it
	// back where it was. Nothing else about it changes.
	SetArchived(ctx context.Context, id string, archived bool) (domain.Product, error)
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

// PasswordResetRepository keeps the links sent by "Esqueci minha senha".
// Tokens arrive already hashed: the token itself never reaches the
// database.
type PasswordResetRepository interface {
	// Create stores a link for userID that expires after ttl, measured by
	// the database's clock, the same one that later checks it.
	Create(ctx context.Context, tokenHash []byte, userID string, ttl time.Duration) error

	// CountRecent returns how many links userID was sent within window.
	CountRecent(ctx context.Context, userID string, window time.Duration) (int, error)

	// GetUserID returns whose link this is, and *domain.NotFoundError when
	// it is unknown, used or expired. It does not use the link up.
	GetUserID(ctx context.Context, tokenHash []byte) (string, error)

	// Consume uses the link up and sets the password in one transaction:
	// passwordHash replaces the old one, the password version goes up,
	// and every other link of the same user goes too. It returns the
	// updated user, or *domain.NotFoundError when the link is unknown,
	// used or expired — including by a concurrent Consume that got there
	// first.
	Consume(ctx context.Context, tokenHash []byte, passwordHash string) (domain.User, error)
}

// SessionRepository is the housekeeping side of the session store. The
// same type also implements scs.CtxStore, which cmd/api hands to the
// session manager; the services only ever need to tidy up.
type SessionRepository interface {
	// DeleteExpired removes sessions past their expiry and returns how
	// many went.
	DeleteExpired(ctx context.Context) (int64, error)
}
