package domain

import (
	"fmt"
	"strconv"
	"time"
)

// Limits on a product's colours and photos. The site shows each colour's
// photos in a gallery: past a dozen nobody scrolls, and the bucket fills
// up.
const (
	MaxColours          = 6
	MaxImagesPerColour  = 12
	MaxImagesPerProduct = 24

	// MaxNewPhotosPerSave is what one save may upload. Every new photo
	// travels in the same request as the form, and the machine spools
	// them while parsing; a piece with many colours is built over several
	// saves.
	MaxNewPhotosPerSave = 12
)

// Image is one photo of a colour of a product, stored in two widths. Its
// place among the colour's photos is Position; the first one is the
// colour's cover.
//
// The widths come from the real layout: a card renders at roughly 300 CSS
// pixels (600 on 2x screens) and the product page at roughly 440 (1200
// covers large displays).
//
// Only the object keys are kept. Public URLs are built from the bucket's
// base URL, which belongs to the configuration, not to each image.
type Image struct {
	ID        string
	ColourID  string
	Key600    string
	Key1200   string
	Position  int32
	CreatedAt time.Time
}

// PhotoEdit is a colour's photo list as the person left it on screen.
// Nothing about photos is saved until the product is: the whole edit is
// applied at once, or not at all.
type PhotoEdit struct {
	// Order is the final list, cover first.
	Order []PhotoSlot
	// Removed lists existing photos to delete. Every photo the colour
	// has must be either in Order or here: one in neither means the list
	// changed elsewhere since the screen was loaded.
	Removed []string
	// New holds the photos being added; a slot refers to one by index.
	New []PhotoSource
	// AppendNew asks to keep the existing photos as they are and add New
	// after them, ignoring Order and Removed. It is what a form submitted
	// without JavaScript can express.
	AppendNew bool
}

// PhotoSlot is one place in the list: an existing photo, or one of the
// photos being added.
type PhotoSlot struct {
	ImageID  string // an existing photo, or empty for a new one
	NewIndex int    // the entry of PhotoEdit.New, when ImageID is empty
}

// PhotoSource gives the bytes of an uploaded photo when asked, so a dozen
// large files never have to sit in memory at once.
type PhotoSource interface {
	Bytes() ([]byte, error)
}

// PhotoErrorField is the ValidationError field for a problem with the new
// photo at index i, so the caller can name the file.
func PhotoErrorField(i int) string { return "photos." + strconv.Itoa(i) }

// Validate checks that the edit is well formed: every new photo placed
// exactly once, no photo twice, nothing both kept and removed, and no more
// than MaxImagesPerColour in the end. An AppendNew edit passes unchecked:
// only the caller knows the photos it appends to, so it resolves the edit
// into an Order first and validates that.
func (e PhotoEdit) Validate() error {
	if e.AppendNew {
		return nil
	}
	malformed := NewValidationError("photos", "Não consegui entender a lista de fotos. Recarregue a página e tente de novo.")

	kept := map[string]bool{}
	placed := make([]bool, len(e.New))
	for _, s := range e.Order {
		if s.ImageID != "" {
			if kept[s.ImageID] {
				return malformed
			}
			kept[s.ImageID] = true
			continue
		}
		if s.NewIndex < 0 || s.NewIndex >= len(e.New) || placed[s.NewIndex] {
			return malformed
		}
		placed[s.NewIndex] = true
	}
	for _, p := range placed {
		if !p {
			return malformed
		}
	}
	for _, id := range e.Removed {
		if kept[id] {
			return malformed
		}
	}

	if len(e.Order) > MaxImagesPerColour {
		return NewValidationError("photos", fmt.Sprintf(
			"Cada cor pode ter até %d fotos. Tire %d antes de salvar.", MaxImagesPerColour, len(e.Order)-MaxImagesPerColour))
	}
	return nil
}
