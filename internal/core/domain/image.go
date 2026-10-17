package domain

import "time"

// Image is one photo of a product, stored in two widths. Its place among
// the product's photos is Position; the first one is the cover.
//
// The widths come from the real layout: a card renders at roughly 300 CSS
// pixels (600 on 2x screens) and the product page at roughly 440 (1200
// covers large displays).
//
// Only the object keys are kept. Public URLs are built from the bucket's
// base URL, which belongs to the configuration, not to each image.
type Image struct {
	ID        string
	ProductID string
	Key600    string
	Key1200   string
	Position  int32
	CreatedAt time.Time
}
