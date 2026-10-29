// Package domain holds the entities and the rules that belong to them.
// It imports nothing but the standard library.
package domain

import (
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Product is a piece in the catalogue.
type Product struct {
	ID   string
	Slug string
	Name string

	// PriceCents is an integer, never a float. In floating point
	// 29.90 * 3 yields 89.69999999999999; prices are entered in reais by
	// the admin and converted to cents at the edge.
	//
	// Zero means the price is not set yet: the owners are cataloguing
	// pieces before pricing them. Nothing is sold for free, so zero is
	// free to mean that, and the column needs no NULL.
	PriceCents int64

	Sizes       []string
	Description string
	// CollectionID is the collection the piece belongs to, empty when it
	// belongs to none. CollectionName comes with it on reads, for showing;
	// writes ignore it.
	CollectionID   string
	CollectionName string
	Details        []string
	SoldOut        bool
	Position       int32

	// Colours are the colours the piece comes in, in display order,
	// each with its photos and stock.
	Colours []Colour

	// ArchivedAt is when the piece left the collection: it no longer
	// shows on the site, but keeps its photos and can come back. Zero
	// means it is in the collection.
	ArchivedAt time.Time

	CreatedAt time.Time
	UpdatedAt time.Time
}

// Archived reports whether the piece is out of the collection.
func (p Product) Archived() bool { return !p.ArchivedAt.IsZero() }

// HasPrice reports whether the price has been set.
func (p Product) HasPrice() bool { return p.PriceCents > 0 }

// Images lists the photos of every colour, colour by colour, in order.
func (p Product) Images() []Image {
	images := []Image{}
	for _, c := range p.Colours {
		images = append(images, c.Images...)
	}
	return images
}

// TracksStock reports whether any of the product's colours counts its
// stock.
func (p Product) TracksStock() bool {
	return slices.ContainsFunc(p.Colours, Colour.TracksStock)
}

// StockTotal adds up the pieces counted in every colour and size.
func (p Product) StockTotal() int32 {
	var total int32
	for _, c := range p.Colours {
		for _, s := range c.Stock {
			total += s.Quantity
		}
	}
	return total
}

// Cover is the photo shown on the catalogue card: the first one of the
// first colour that has any.
func (p Product) Cover() (Image, bool) {
	for _, c := range p.Colours {
		if len(c.Images) > 0 {
			return c.Images[0], true
		}
	}
	return Image{}, false
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

// AllSizes lists the sizes a product can come in, in display order. A
// closed list, because free typing turns into "G", "g " and "Grande".
func AllSizes() []string {
	return []string{"P", "M", "G", "GG"}
}

// Normalized trims what people type and puts the lists in a consistent
// shape: sizes in display order without repeats, and no blank details.
func (p Product) Normalized() Product {
	p.Name = strings.TrimSpace(p.Name)
	p.Description = strings.TrimSpace(p.Description)
	p.CollectionID = strings.TrimSpace(p.CollectionID)

	var sizes []string
	for _, s := range AllSizes() {
		if slices.Contains(p.Sizes, s) {
			sizes = append(sizes, s)
		}
	}
	for _, s := range p.Sizes {
		if !slices.Contains(AllSizes(), s) && !slices.Contains(sizes, s) {
			sizes = append(sizes, s)
		}
	}
	p.Sizes = sizes

	var details []string
	for _, d := range p.Details {
		if d = strings.TrimSpace(d); d != "" {
			details = append(details, d)
		}
	}
	p.Details = details
	return p
}

// Validate checks the rules the database constraints cannot express well
// enough to produce a decent message. It reports every problem at once,
// joined, so the form can mark all the fields in one go instead of one
// per attempt.
func (p Product) Validate() error {
	var errs []error
	if strings.TrimSpace(p.Name) == "" {
		errs = append(errs, NewValidationError("name", "Falta o nome da peça"))
	}

	if p.PriceCents < 0 {
		errs = append(errs, NewValidationError("price", "O preço não pode ser negativo"))
	}

	if len(p.Sizes) == 0 {
		errs = append(errs, NewValidationError("sizes", "Escolha ao menos um tamanho"))
	}
	for _, s := range p.Sizes {
		if !slices.Contains(AllSizes(), s) {
			errs = append(errs, NewValidationError("sizes", "Tamanho desconhecido: "+s))
			break
		}
	}

	// Without a name there is nothing to build the slug from, and the
	// missing name already says so.
	if p.Slug == "" && strings.TrimSpace(p.Name) != "" {
		errs = append(errs, NewValidationError("slug", "Não consegui gerar o endereço a partir do nome"))
	}
	return errors.Join(errs...)
}
