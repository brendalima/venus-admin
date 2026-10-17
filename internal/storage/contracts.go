// Package storage holds the adapters for services outside the database.
// None is implemented yet: the contracts describe the milestones ahead.
package storage

import "context"

// ImageStore keeps the photo files (Cloudflare R2).
type ImageStore interface {
	// Put stores one processed image and returns its public URL.
	Put(ctx context.Context, key string, data []byte, contentType string) (string, error)
	Delete(ctx context.Context, key string) error
}

// ImageProcessor turns an uploaded file into the widths the site serves.
//
// Re-encoding is not only about file size: it strips EXIF, which on
// product photos routinely carries the GPS coordinates of the shoot.
type ImageProcessor interface {
	Process(original []byte) (small, large []byte, err error)
}

// SitePublisher triggers a rebuild of the static site (Cloudflare Pages
// deploy hook).
type SitePublisher interface {
	Publish(ctx context.Context) error
}
