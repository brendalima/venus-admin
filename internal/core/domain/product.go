// Package catalog is the core of the hexagon: it holds the domain model
// and the rules around it.
//
// It must not import any adapter, driver or framework. Everything it
// needs from the outside world is declared as an interface in ports.go
// and supplied by the composition root.
package catalog

import (
	"regexp"
	"strings"
	"time"
)

// Product is a piece in the catalogue.
type Product struct {
	ID   int64
	Slug string
	Name string

	// PriceCents is an integer, never a float. In floating point
	// 29.90 * 3 yields 89.69999999999999; prices are entered in reais by
	// the admin and converted to cents at the edge.
	PriceCents int64

	Sizes       []string
	Description string
	Details     []string
	SoldOut     bool
	Position    int32

	Images []Image

	CreatedAt time.Time
	UpdatedAt time.Time
}

// View is the angle a photo was taken from. The set is closed on purpose:
// free text turns into "back", "Back ", "rear" across three people.
type View string

const (
	ViewFront    View = "Frente"
	ViewBack     View = "Costas"
	ViewSide     View = "Perfil"
	ViewFullBody View = "Corpo inteiro"
	ViewCloseUp  View = "Detalhe"
)

// Views lists every accepted angle, in the order the admin shows them.
// The values are the labels the shop owners read, so they stay in
// Portuguese while the code around them is in English.
func Views() []View {
	return []View{ViewFront, ViewBack, ViewSide, ViewFullBody, ViewCloseUp}
}

func (v View) Valid() bool {
	for _, known := range Views() {
		if v == known {
			return true
		}
	}
	return false
}

// Image is one photo of a product, stored in two widths.
//
// The widths come from the real layout: a card renders at roughly 300 CSS
// pixels (600 on 2x screens) and the product page at roughly 440 (1200
// covers large displays).
type Image struct {
	ID        int64
	ProductID int64
	View      View
	URL600    string
	URL1200   string
	Key600    string
	Key1200   string
	Position  int32
	CreatedAt time.Time
}

// Cover is the photo shown on the catalogue card: the first one.
func (p Product) Cover() (Image, bool) {
	if len(p.Images) == 0 {
		return Image{}, false
	}
	return p.Images[0], true
}

var (
	nonSlugChars = regexp.MustCompile(`[^a-z0-9]+`)
	accents      = strings.NewReplacer(
		"á", "a", "à", "a", "â", "a", "ã", "a", "ä", "a",
		"é", "e", "è", "e", "ê", "e", "ë", "e",
		"í", "i", "ì", "i", "î", "i", "ï", "i",
		"ó", "o", "ò", "o", "ô", "o", "õ", "o", "ö", "o",
		"ú", "u", "ù", "u", "û", "u", "ü", "u",
		"ç", "c", "ñ", "n",
	)
)

// Slugify derives a URL segment from a product name.
//
// It exists so nobody using the admin has to know what a slug is: they
// type "Biquíni Aurora" and get "biquini-aurora".
func Slugify(name string) string {
	s := accents.Replace(strings.ToLower(strings.TrimSpace(name)))
	s = nonSlugChars.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}
