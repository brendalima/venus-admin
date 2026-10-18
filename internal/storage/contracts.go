// Package storage holds the adapters for services outside the database
// (object storage, email), and the image processing that prepares photos
// for them.
package storage

import (
	"context"

	"github.com/brendalima/venus-admin/internal/core/domain"
)

// ImageStore keeps the photo files (Cloudflare R2). Callers keep only the
// key; the public URL is built from the bucket's base URL.
type ImageStore interface {
	// Put stores one processed image under key.
	Put(ctx context.Context, key string, data []byte, contentType string) error
	Delete(ctx context.Context, key string) error
}

// ImageProcessor turns an uploaded file into the widths the site serves.
//
// Re-encoding is not only about file size: it strips EXIF, which on
// product photos routinely carries the GPS coordinates of the shoot.
//
// A file that is not a usable photo comes back as *domain.ValidationError,
// with a message for the person who uploaded it.
type ImageProcessor interface {
	Process(ctx context.Context, original []byte) (small, large []byte, err error)
}

// SitePublisher triggers a rebuild of the static site (Cloudflare Pages
// deploy hook).
type SitePublisher interface {
	Publish(ctx context.Context) error
}

// Mailer delivers email (Resend in production).
type Mailer interface {
	Send(ctx context.Context, e domain.Email) error
}
