// Package domain holds the entities and the rules that belong to them.
// It imports nothing but the standard library.
package domain

import (
	"regexp"
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

// Validate checks the rules the database constraints cannot express well
// enough to produce a decent message.
func (p Product) Validate() error {
	switch {
	case len(p.Name) == 0:
		return NewValidationError("name", "Falta o nome da peça")
	case p.PriceCents < 0:
		return NewValidationError("price", "O preço não pode ser negativo")
	case p.PriceCents == 0:
		return NewValidationError("price", "Falta o preço")
	case len(p.Sizes) == 0:
		return NewValidationError("sizes", "Escolha ao menos um tamanho")
	case p.Slug == "":
		return NewValidationError("slug", "Não consegui gerar o endereço a partir do nome")
	}

	for _, img := range p.Images {
		if !img.View.Valid() {
			return NewValidationError("images", "Ângulo de foto desconhecido: "+string(img.View))
		}
	}
	return nil
}
