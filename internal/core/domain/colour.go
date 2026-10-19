package domain

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// MaxQuantity guards against a slip of the finger: nobody stocks a
// thousand of one piece in one size and colour.
const MaxQuantity = 999

// Colour is one colour a product comes in, with its own photos and its own
// stock. Every product has at least one.
type Colour struct {
	ID        string
	ProductID string
	Name      string
	Position  int32

	// Images are the colour's photos, cover first.
	Images []Image

	// Stock is sparse on purpose, the way the owners write it down
	// ("P 1x Bege, M 2x Bege, G indisponível"):
	//   - no entries at all: stock is not tracked for this colour, and the
	//     site shows the sizes without numbers;
	//   - an entry: that many pieces of that size;
	//   - a size without an entry, or at zero, while others have one:
	//     unavailable.
	// Entries follow the product's size order.
	Stock []Stock

	CreatedAt time.Time
}

// Stock is how many pieces of one size a colour has.
type Stock struct {
	Size     string
	Quantity int32
}

// TracksStock reports whether the colour's quantities are being counted.
// It is inferred from there being any entry at all, so the site is told
// rather than left to work it out.
func (c Colour) TracksStock() bool { return len(c.Stock) > 0 }

// Quantity is how many pieces of size the colour has. A size without an
// entry has none, which only means "unavailable" when TracksStock.
func (c Colour) Quantity(size string) int32 {
	for _, s := range c.Stock {
		if s.Size == size {
			return s.Quantity
		}
	}
	return 0
}

// ColourField is the ValidationError field for field of the colour at
// index n of a ColoursEdit, such as "colours.1.name".
func ColourField(n int, field string) string {
	return "colours." + strconv.Itoa(n) + "." + field
}

// ColoursEdit is a product's colours as the person left them on screen.
// Like photos, colours are saved with the product: the whole edit is
// applied at once, or not at all.
type ColoursEdit struct {
	// Colours is the final list, in order. Blank ones are skipped (see
	// ColourEdit.Blank), and the others keep their index, which is how
	// errors name them.
	Colours []ColourEdit
	// Removed lists existing colours to delete, photos and stock
	// included. Every colour the product has must be either in Colours
	// or here: one in neither means the list changed elsewhere since the
	// screen was loaded.
	Removed []string
}

// ColourEdit is one colour of a ColoursEdit.
type ColourEdit struct {
	ID   string // an existing colour, or empty for a new one
	Name string
	// Stock replaces the colour's stock entirely: a size left out is no
	// longer counted.
	Stock  []Stock
	Photos PhotoEdit
}

// Blank reports whether c is a new colour nobody filled in: no name, no
// photo, no quantity. The form offers one ready to fill, and leaving it
// alone must not get in the way of saving.
func (c ColourEdit) Blank() bool {
	return c.ID == "" && strings.TrimSpace(c.Name) == "" && len(c.Stock) == 0 &&
		len(c.Photos.Order) == 0 && len(c.Photos.New) == 0
}

// Normalized trims the names, and keeps each colour's stock to the sizes
// in sizes, the product's, once each and in that order. A quantity for a
// size the product no longer comes in means nothing.
func (e ColoursEdit) Normalized(sizes []string) ColoursEdit {
	colours := make([]ColourEdit, len(e.Colours))
	for n, c := range e.Colours {
		c.Name = strings.TrimSpace(c.Name)
		var stock []Stock
		for _, size := range sizes {
			if i := slices.IndexFunc(c.Stock, func(s Stock) bool { return s.Size == size }); i >= 0 {
				stock = append(stock, c.Stock[i])
			}
		}
		c.Stock = stock
		colours[n] = c
	}
	e.Colours = colours
	return e
}

// Validate checks what the person typed for each colour: names and
// quantities, and that there are some colours and not too many. It
// reports every problem at once, joined, as Product.Validate does.
// ValidateLists checks the rest.
func (e ColoursEdit) Validate() error {
	var errs []error
	names := map[string]bool{}
	colours := 0
	for n, c := range e.Colours {
		if c.Blank() {
			continue
		}
		colours++

		name := strings.ToLower(strings.TrimSpace(c.Name))
		switch {
		case name == "":
			errs = append(errs, NewValidationError(ColourField(n, "name"), "Falta o nome da cor"))
		case names[name]:
			errs = append(errs, NewValidationError(ColourField(n, "name"), "Já existe outra cor com esse nome nesta peça"))
		}
		names[name] = true

		sizes := map[string]bool{}
		for _, s := range c.Stock {
			field := ColourField(n, "stock."+s.Size)
			switch {
			case sizes[s.Size]:
				errs = append(errs, NewValidationError(field, "Esse tamanho apareceu duas vezes. Recarregue a página e tente de novo."))
			case s.Quantity < 0:
				errs = append(errs, NewValidationError(field, "A quantidade não pode ser negativa"))
			case s.Quantity > MaxQuantity:
				errs = append(errs, NewValidationError(field, fmt.Sprintf("A quantidade passa de %d. Confira o número.", MaxQuantity)))
			}
			sizes[s.Size] = true
		}
	}

	switch {
	case colours == 0:
		errs = append(errs, NewValidationError("colours", "Cadastre ao menos uma cor"))
	case colours > MaxColours:
		errs = append(errs, NewValidationError("colours", fmt.Sprintf(
			"Cada peça pode ter até %d cores. Tire %d antes de salvar.", MaxColours, colours-MaxColours)))
	}
	return errors.Join(errs...)
}

// ValidateLists checks that the lists are well formed, no colour twice
// nor both kept and removed, and each colour's photo edit too, and that
// the photos stay within their limits. A problem here is worth knowing
// before processing any photo. Each photo edit must have AppendNew
// resolved already.
func (e ColoursEdit) ValidateLists() error {
	malformed := NewValidationError("colours", "Não consegui entender a lista de cores. Recarregue a página e tente de novo.")

	var errs []error
	kept := map[string]bool{}
	photos, newPhotos := 0, 0
	for n, c := range e.Colours {
		if c.Blank() {
			continue
		}
		if c.ID != "" {
			if kept[c.ID] {
				return malformed
			}
			kept[c.ID] = true
		}
		photos += len(c.Photos.Order)
		newPhotos += len(c.Photos.New)

		var invalid *ValidationError
		if err := c.Photos.Validate(); errors.As(err, &invalid) {
			errs = append(errs, NewValidationError(ColourField(n, invalid.Field), invalid.Message))
		}
	}
	for _, id := range e.Removed {
		if kept[id] {
			return malformed
		}
	}

	if photos > MaxImagesPerProduct {
		errs = append(errs, NewValidationError("photos", fmt.Sprintf(
			"Cada peça pode ter até %d fotos, somando todas as cores. Tire %d antes de salvar.",
			MaxImagesPerProduct, photos-MaxImagesPerProduct)))
	}
	if newPhotos > MaxNewPhotosPerSave {
		errs = append(errs, NewValidationError("photos", fmt.Sprintf(
			"Dá para enviar até %d fotos novas de cada vez. Salve com essas e acrescente as outras depois.",
			MaxNewPhotosPerSave)))
	}
	return errors.Join(errs...)
}

// ColourChanges tells the repository what a save adds and removes among a
// product's colours and photos. Anything else it is given must already
// exist, or the product changed since the screen was loaded.
type ColourChanges struct {
	AddedColours   []string
	RemovedColours []string
	AddedImages    []string
	RemovedImages  []string
}
