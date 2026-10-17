package domain

import "time"

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
