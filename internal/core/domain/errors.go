package catalog

import "errors"

// Domain errors. Adapters translate these into their own vocabulary —
// the HTTP adapter into status codes, for instance — so the core never
// has to know what a 404 is.
var (
	ErrNotFound     = errors.New("product not found")
	ErrSlugTaken    = errors.New("slug already in use")
	ErrInvalidInput = errors.New("invalid product")
)

// ValidationError carries the field that failed and a message meant for
// the person using the admin, which is why the message is in Portuguese
// while the code around it is in English.
type ValidationError struct {
	Field   string
	Message string
}

func (e ValidationError) Error() string { return e.Field + ": " + e.Message }

func (e ValidationError) Is(target error) bool { return target == ErrInvalidInput }

// Validate checks the rules the database constraints cannot express well
// enough to produce a decent message.
func (p Product) Validate() error {
	switch {
	case len(p.Name) == 0:
		return ValidationError{Field: "name", Message: "Falta o nome da peça"}
	case p.PriceCents < 0:
		return ValidationError{Field: "price", Message: "O preço não pode ser negativo"}
	case p.PriceCents == 0:
		return ValidationError{Field: "price", Message: "Falta o preço"}
	case len(p.Sizes) == 0:
		return ValidationError{Field: "sizes", Message: "Escolha ao menos um tamanho"}
	case p.Slug == "":
		return ValidationError{Field: "slug", Message: "Não consegui gerar o endereço a partir do nome"}
	}

	for _, img := range p.Images {
		if !img.View.Valid() {
			return ValidationError{Field: "images", Message: "Ângulo de foto desconhecido: " + string(img.View)}
		}
	}
	return nil
}
