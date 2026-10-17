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
	Details     []string
	SoldOut     bool
	Position    int32

	Images []Image

	CreatedAt time.Time
	UpdatedAt time.Time
}

// HasPrice reports whether the price has been set.
func (p Product) HasPrice() bool { return p.PriceCents > 0 }

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
