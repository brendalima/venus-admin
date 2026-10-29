package domain

import (
	"errors"
	"slices"
	"testing"
)

func TestColourTracksStock(t *testing.T) {
	if (Colour{}).TracksStock() {
		t.Error("a colour without stock entries tracks stock")
	}
	// Zero is a count: the size is unavailable, not untracked.
	if !(Colour{Stock: []Stock{{Size: "G", Quantity: 0}}}).TracksStock() {
		t.Error("a colour with an entry at zero does not track stock")
	}
}

func TestColoursEditNormalized(t *testing.T) {
	in := ColoursEdit{Colours: []ColourEdit{{
		Name:  "  Bege ",
		Hexes: []string{" C8A27C", "", " "},
		Stock: []Stock{{Size: "G", Quantity: 1}, {Size: "GG", Quantity: 4}, {Size: "P", Quantity: 2}, {Size: "G", Quantity: 9}},
	}}}

	got := in.Normalized([]string{"P", "M", "G"})

	c := got.Colours[0]
	if c.Name != "Bege" {
		t.Errorf("name = %q, want it trimmed", c.Name)
	}
	if !slices.Equal(c.Hexes, []string{"#c8a27c"}) {
		t.Errorf("hexes = %q, want #c8a27c alone", c.Hexes)
	}
	// GG is not in the grid; the second G is a repeat.
	want := []Stock{{Size: "P", Quantity: 2}, {Size: "G", Quantity: 1}}
	if !slices.Equal(c.Stock, want) {
		t.Errorf("stock = %v, want %v", c.Stock, want)
	}
	if in.Colours[0].Name != "  Bege " || len(in.Colours[0].Stock) != 4 {
		t.Error("the input was modified")
	}
}

func TestNormalizeHex(t *testing.T) {
	tests := []struct{ in, want string }{
		{"", ""},
		{"   ", ""},
		{"#c8a27c", "#c8a27c"},
		{" #C8A27C ", "#c8a27c"},
		{"C8A27C", "#c8a27c"},
		{"bege", "#bege"}, // left for ValidHex to refuse
	}
	for _, tt := range tests {
		if got := NormalizeHex(tt.in); got != tt.want {
			t.Errorf("NormalizeHex(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestValidHex(t *testing.T) {
	for _, hex := range []string{"#000000", "#c8a27c", "#ffffff"} {
		if !ValidHex(hex) {
			t.Errorf("ValidHex(%q) = false", hex)
		}
	}
	for _, hex := range []string{"", "#", "c8a27c", "#c8a27", "#c8a27c0", "#C8A27C", "#g8a27c", "#c8a27é"} {
		if ValidHex(hex) {
			t.Errorf("ValidHex(%q) = true", hex)
		}
	}
}

func TestColoursEditValidate(t *testing.T) {
	newPhotos := func(n int) PhotoEdit {
		var e PhotoEdit
		for i := range n {
			e.New = append(e.New, fakeSource{})
			e.Order = append(e.Order, PhotoSlot{NewIndex: i})
		}
		return e
	}
	named := func(names ...string) []ColourEdit {
		var cs []ColourEdit
		for _, n := range names {
			cs = append(cs, ColourEdit{Name: n})
		}
		return cs
	}

	tests := []struct {
		name       string
		edit       ColoursEdit
		wantFields []string // nil when valid
	}{
		{name: "one colour", edit: ColoursEdit{Colours: named("Bege")}},
		{
			name: "stock and photos",
			edit: ColoursEdit{Colours: []ColourEdit{{
				ID: "a", Name: "Bege",
				Stock:  []Stock{{Size: "P", Quantity: 0}, {Size: "M", Quantity: MaxQuantity}},
				Photos: PhotoEdit{Order: []PhotoSlot{{ImageID: "x"}}},
			}}, Removed: []string{"b"}},
		},
		{
			// The block the form offers ready to fill, left alone.
			name: "a blank new colour is skipped",
			edit: ColoursEdit{Colours: []ColourEdit{{Name: "Bege"}, {Name: "  "}}},
		},
		{name: "no colours", edit: ColoursEdit{}, wantFields: []string{"colours"}},
		{name: "only blank colours", edit: ColoursEdit{Colours: named("", "")}, wantFields: []string{"colours"}},
		{
			// An existing colour is never blank: erasing its name is a
			// mistake to point out, not a way to delete it.
			name:       "existing colour without a name",
			edit:       ColoursEdit{Colours: []ColourEdit{{Name: "Bege"}, {ID: "a"}}},
			wantFields: []string{"colours.1.name"},
		},
		{
			name:       "new colour with stock but no name",
			edit:       ColoursEdit{Colours: []ColourEdit{{Stock: []Stock{{Size: "P", Quantity: 1}}}}},
			wantFields: []string{"colours.0.name"},
		},
		{
			name:       "same name twice, in other case",
			edit:       ColoursEdit{Colours: named("Bege", "Vinho", " bege")},
			wantFields: []string{"colours.2.name"},
		},
		{
			name: "quantities out of range",
			edit: ColoursEdit{Colours: []ColourEdit{{
				Name: "Bege", Stock: []Stock{{Size: "P", Quantity: -1}, {Size: "M", Quantity: MaxQuantity + 1}},
			}}},
			wantFields: []string{"colours.0.stock.P", "colours.0.stock.M"},
		},
		{
			name: "main and secondary hex codes",
			edit: ColoursEdit{Colours: []ColourEdit{{Name: "Bege", Hexes: []string{"#c8a27c", "#ffffff", "#000000"}}}},
		},
		{
			name: "hex codes that are not colours",
			edit: ColoursEdit{Colours: []ColourEdit{
				{Name: "Bege", Hexes: []string{"#c8a27"}}, {Name: "Vinho", Hexes: []string{"#5a1a2b", "#vinho1"}}, {Name: "Azul", Hexes: []string{"#C8A27C"}},
			}},
			wantFields: []string{"colours.0.hex.0", "colours.1.hex.1", "colours.2.hex.0"},
		},
		{
			name:       "too many hex codes",
			edit:       ColoursEdit{Colours: []ColourEdit{{Name: "Bege", Hexes: []string{"#000000", "#111111", "#222222", "#333333"}}}},
			wantFields: []string{"colours.0.hex"},
		},
		{
			// A colour picked and nothing else is something to finish,
			// not a block to skip.
			name:       "new colour with a hex code but no name",
			edit:       ColoursEdit{Colours: []ColourEdit{{Hexes: []string{"#c8a27c"}}}},
			wantFields: []string{"colours.0.name"},
		},
		{
			name: "a blank hex row leaves a new colour blank",
			edit: ColoursEdit{Colours: []ColourEdit{{Name: "Bege"}, {Hexes: []string{" "}}}},
		},
		{
			name:       "too many colours",
			edit:       ColoursEdit{Colours: named("1", "2", "3", "4", "5", "6", "7")},
			wantFields: []string{"colours"},
		},
		{
			name: "too many photos in a colour",
			edit: ColoursEdit{Colours: []ColourEdit{
				{Name: "Bege"}, {Name: "Vinho", Photos: newPhotos(MaxImagesPerColour + 1)},
			}},
			wantFields: []string{"colours.1.photos", "photos"},
		},
		{
			name: "too many photos in the product",
			edit: ColoursEdit{Colours: []ColourEdit{
				{Name: "Bege", Photos: PhotoEdit{Order: make([]PhotoSlot, 0)}},
				{Name: "Vinho", Photos: keptPhotos(MaxImagesPerColour)},
				{Name: "Verde", Photos: keptPhotos(MaxImagesPerColour)},
				{Name: "Azul", Photos: keptPhotos(1)},
			}},
			wantFields: []string{"photos"},
		},
		{
			name: "too many new photos in one save",
			edit: ColoursEdit{Colours: []ColourEdit{
				{Name: "Bege", Photos: newPhotos(MaxNewPhotosPerSave)}, {Name: "Vinho", Photos: newPhotos(1)},
			}},
			wantFields: []string{"photos"},
		},
		{
			name: "every problem at once",
			edit: ColoursEdit{Colours: []ColourEdit{
				{ID: "a"}, {Name: "Vinho", Stock: []Stock{{Size: "G", Quantity: -2}}},
			}},
			wantFields: []string{"colours.0.name", "colours.1.stock.G"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validationFields(errors.Join(tt.edit.Validate(), tt.edit.ValidateLists()))
			if !slices.Equal(got, tt.wantFields) {
				t.Errorf("errors on %v, want %v", got, tt.wantFields)
			}
		})
	}
}

func TestColoursEditValidateMalformed(t *testing.T) {
	for name, edit := range map[string]ColoursEdit{
		"the same colour twice":    {Colours: []ColourEdit{{ID: "a", Name: "Bege"}, {ID: "a", Name: "Vinho"}}},
		"kept and removed":         {Colours: []ColourEdit{{ID: "a", Name: "Bege"}}, Removed: []string{"a"}},
		"the same size twice":      {Colours: []ColourEdit{{Name: "Bege", Stock: []Stock{{Size: "P"}, {Size: "P"}}}}},
		"a photo list nonsensical": {Colours: []ColourEdit{{Name: "Bege", Photos: PhotoEdit{Order: []PhotoSlot{{NewIndex: 3}}}}}},
	} {
		t.Run(name, func(t *testing.T) {
			var v *ValidationError
			if err := errors.Join(edit.Validate(), edit.ValidateLists()); !errors.As(err, &v) || v.Message == "" {
				t.Errorf("err = %v, want a ValidationError with a message", err)
			}
		})
	}
}

func keptPhotos(n int) PhotoEdit {
	var e PhotoEdit
	for i := range n {
		e.Order = append(e.Order, PhotoSlot{ImageID: string(rune('a' + i))})
	}
	return e
}

// validationFields lists the fields of the validation errors joined in
// err, in order.
func validationFields(err error) []string {
	var fields []string
	var walk func(error)
	walk = func(err error) {
		var joined interface{ Unwrap() []error }
		if errors.As(err, &joined) {
			for _, e := range joined.Unwrap() {
				walk(e)
			}
			return
		}
		var v *ValidationError
		if errors.As(err, &v) {
			fields = append(fields, v.Field)
		}
	}
	walk(err)
	return fields
}

func TestStockTotals(t *testing.T) {
	bege := Colour{Name: "Bege", Stock: []Stock{{Size: "P", Quantity: 1}, {Size: "M", Quantity: 2}}}
	vinho := Colour{Name: "Vinho", Stock: []Stock{{Size: "P", Quantity: 3}}}
	if bege.Quantity("M") != 2 || bege.Quantity("G") != 0 {
		t.Errorf("quantities M %d, G %d, want 2 and 0", bege.Quantity("M"), bege.Quantity("G"))
	}

	p := Product{Colours: []Colour{bege, {Name: "Preto"}, vinho}}
	if !p.TracksStock() || p.StockTotal() != 6 {
		t.Errorf("tracks %v, total %d, want true and 6", p.TracksStock(), p.StockTotal())
	}
	if (Product{Colours: []Colour{{Name: "Única"}}}).TracksStock() {
		t.Error("a product whose colours have no entries tracks stock")
	}
}
