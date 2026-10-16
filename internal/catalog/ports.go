package catalog

import "context"

// The interfaces below are the hexagon's driven ports: what the core
// needs from the outside. They live here, in the core, so that the core
// owns the contract and the adapters depend on it — never the reverse.

// Repository persists products. Implemented by adapter/postgres.
type Repository interface {
	List(ctx context.Context) ([]Product, error)
	Get(ctx context.Context, id int64) (Product, error)
	GetBySlug(ctx context.Context, slug string) (Product, error)
	Create(ctx context.Context, p Product) (Product, error)
	Update(ctx context.Context, p Product) (Product, error)
	Delete(ctx context.Context, id int64) error

	// HasUnpublishedChanges reports whether any product changed after the
	// last publish, which is what drives the "pending changes" badge.
	HasUnpublishedChanges(ctx context.Context) (bool, error)
	MarkPublished(ctx context.Context) error
}

// ImageStore keeps the photo files. Implemented by adapter/r2.
type ImageStore interface {
	// Put stores one processed image and returns its public URL.
	Put(ctx context.Context, key string, data []byte, contentType string) (string, error)
	Delete(ctx context.Context, key string) error
}

// ImageProcessor turns an uploaded file into the widths the site serves.
// Implemented by adapter/vips.
//
// Re-encoding is not only about file size: it strips EXIF, which on
// product photos routinely carries the GPS coordinates of the shoot.
type ImageProcessor interface {
	Process(original []byte) (small, large []byte, err error)
}

// SitePublisher triggers a rebuild of the static site.
// Implemented by adapter/cloudflare.
type SitePublisher interface {
	Publish(ctx context.Context) error
}
