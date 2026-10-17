package domain

import (
	"errors"
	"testing"
)

func TestSlugify(t *testing.T) {
	// The admin generates the slug from the name so nobody using it has
	// to know what a slug is.
	tests := []struct{ name, want string }{
		{"Biquíni Aurora", "biquini-aurora"},
		{"Maiô Órion", "maio-orion"},
		{"Saída de Praia Halo", "saida-de-praia-halo"},
		{"  Espaços   Extras  ", "espacos-extras"},
		{"Açaí & Côco", "acai-coco"},
		{"UPPERCASE", "uppercase"},
		{"---", ""},
	}

	for _, tt := range tests {
		if got := Slugify(tt.name); got != tt.want {
			t.Errorf("Slugify(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestViewValidation(t *testing.T) {
	if !ViewFront.Valid() {
		t.Error("ViewFront should be valid")
	}
	if View("atrás").Valid() {
		t.Error("free text should not be a valid view")
	}
	if len(Views()) != 5 {
		t.Errorf("Views() returned %d entries, want 5", len(Views()))
	}
}

func TestCover(t *testing.T) {
	empty := Product{}
	if _, ok := empty.Cover(); ok {
		t.Error("a product with no images has no cover")
	}

	p := Product{Images: []Image{
		{View: ViewFront, URL600: "first"},
		{View: ViewBack, URL600: "second"},
	}}
	cover, ok := p.Cover()
	if !ok {
		t.Fatal("expected a cover")
	}
	if cover.URL600 != "first" {
		t.Errorf("cover = %q, want the first image", cover.URL600)
	}
}

func TestValidate(t *testing.T) {
	valid := Product{
		Slug:       "biquini-aurora",
		Name:       "Biquíni Aurora",
		PriceCents: 18900,
		Sizes:      []string{"P"},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid product rejected: %v", err)
	}

	tests := []struct {
		name  string
		edit  func(*Product)
		field string
	}{
		{"missing name", func(p *Product) { p.Name = "" }, "name"},
		{"zero price", func(p *Product) { p.PriceCents = 0 }, "price"},
		{"negative price", func(p *Product) { p.PriceCents = -1 }, "price"},
		{"no sizes", func(p *Product) { p.Sizes = nil }, "sizes"},
		{"missing slug", func(p *Product) { p.Slug = "" }, "slug"},
		{"unknown view", func(p *Product) {
			p.Images = []Image{{View: View("atrás")}}
		}, "images"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := valid
			tt.edit(&p)

			err := p.Validate()
			if err == nil {
				t.Fatal("expected a validation error")
			}

			var ve *ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("error is not a ValidationError: %v", err)
			}
			if ve.Field != tt.field {
				t.Errorf("field = %q, want %q", ve.Field, tt.field)
			}
			// The message is read by the shop owners, so it must be
			// something other than an empty string or a Go error.
			if ve.Message == "" {
				t.Error("validation message is empty")
			}
		})
	}
}
