package services

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/brendalima/venus-admin/internal/core/domain"
)

// withTwoColours is existing with Bege (photos a and b) and Vinho (photo
// c, and P counted).
func withTwoColours(f *photoFixture) (bege, vinho domain.Colour) {
	p := f.repo.products[existing.ID]
	p.Sizes = []string{"P", "M"}
	p.Colours = append(p.Colours, domain.Colour{
		ID: uuid.New().String(), Name: "Vinho", Stock: []domain.Stock{{Size: "P", Quantity: 2}},
	})
	f.repo.products[existing.ID] = p
	addImages(f, 0, "a", "b")
	addImages(f, 1, "c")
	p = f.repo.products[existing.ID]
	return p.Colours[0], p.Colours[1]
}

// keep is the edit of a stored colour that leaves it as it is.
func keep(c domain.Colour) domain.ColourEdit {
	e := domain.ColourEdit{ID: c.ID, Name: c.Name, Stock: c.Stock}
	for _, img := range c.Images {
		e.Photos.Order = append(e.Photos.Order, domain.PhotoSlot{ImageID: img.ID})
	}
	return e
}

func TestCreateWithColours(t *testing.T) {
	f := newPhotoFixture()
	in := validInput() // sizes P and G
	p, err := f.service.Create(context.Background(), in, domain.ColoursEdit{Colours: []domain.ColourEdit{
		{
			Name:   " Bege ",
			Stock:  []domain.Stock{{Size: "G", Quantity: 0}, {Size: "M", Quantity: 5}, {Size: "P", Quantity: 1}},
			Photos: domain.PhotoEdit{Order: []domain.PhotoSlot{{NewIndex: 0}}, New: []domain.PhotoSource{source("bege")}},
		},
		{}, // the block offered ready to fill, left alone
		{Name: "Vinho", Photos: domain.PhotoEdit{AppendNew: true, New: []domain.PhotoSource{source("vinho")}}},
	}})
	if err != nil {
		t.Fatal(err)
	}

	if len(p.Colours) != 2 || p.Colours[0].Name != "Bege" || p.Colours[1].Name != "Vinho" {
		t.Fatalf("colours %+v, want Bege and Vinho", p.Colours)
	}
	if p.Colours[0].ID == p.Colours[1].ID || uuid.Validate(p.Colours[1].ID) != nil {
		t.Errorf("colour IDs %q and %q, want two generated UUIDs", p.Colours[0].ID, p.Colours[1].ID)
	}
	// M is not one of the piece's sizes; the rest follow its order.
	if want := []domain.Stock{{Size: "P", Quantity: 1}, {Size: "G", Quantity: 0}}; !slices.Equal(p.Colours[0].Stock, want) {
		t.Errorf("Bege stock %v, want %v", p.Colours[0].Stock, want)
	}
	for i, want := range []string{"bege", "vinho"} {
		imgs := p.Colours[i].Images
		if len(imgs) != 1 || f.store.files[imgs[0].Key1200] != "image/jpeg:large "+want {
			t.Errorf("colour %d photos %+v, want the %s one", i, imgs, want)
		}
	}
}

func TestUpdateColours(t *testing.T) {
	f := newPhotoFixture(existing)
	bege, vinho := withTwoColours(f)
	a, b, c := bege.Images[0], bege.Images[1], vinho.Images[0]

	// Vinho goes, photo and all. Azul is new and first, with a photo.
	// Bege is renamed, loses a, and starts counting M.
	edited := keep(bege)
	edited.Name = "Bege Claro"
	edited.Photos = domain.PhotoEdit{Order: []domain.PhotoSlot{{ImageID: b.ID}}, Removed: []string{a.ID}}
	edited.Stock = []domain.Stock{{Size: "M", Quantity: 4}}
	// The stored product, whose sizes include M.
	updated, err := f.service.Update(context.Background(), f.repo.products[existing.ID], &domain.ColoursEdit{
		Colours: []domain.ColourEdit{
			{Name: "Azul", Photos: domain.PhotoEdit{Order: []domain.PhotoSlot{{NewIndex: 0}}, New: []domain.PhotoSource{source("azul")}}},
			edited,
		},
		Removed: []string{vinho.ID},
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(updated.Colours) != 2 {
		t.Fatalf("colours %+v, want two", updated.Colours)
	}
	azul, kept := updated.Colours[0], updated.Colours[1]
	if azul.Name != "Azul" || kept.ID != bege.ID || kept.Name != "Bege Claro" {
		t.Errorf("colours %+v, want Azul then Bege Claro", updated.Colours)
	}
	if !slices.Equal(ids(kept.Images), []string{b.ID}) || !slices.Equal(kept.Stock, edited.Stock) {
		t.Errorf("Bege Claro %+v, want photo b and M counted", kept)
	}

	want := domain.ColourChanges{
		AddedColours:   []string{azul.ID},
		RemovedColours: []string{vinho.ID},
		AddedImages:    []string{azul.Images[0].ID},
		RemovedImages:  []string{a.ID},
	}
	got := f.repo.changes
	if !slices.Equal(got.AddedColours, want.AddedColours) || !slices.Equal(got.RemovedColours, want.RemovedColours) ||
		!slices.Equal(got.AddedImages, want.AddedImages) || !slices.Equal(got.RemovedImages, want.RemovedImages) {
		t.Errorf("changes %+v, want %+v", got, want)
	}
	// The removed photo's files go, and so do the removed colour's.
	for _, img := range []domain.Image{a, c} {
		if _, ok := f.store.files[img.Key1200]; ok {
			t.Errorf("files of %s are still stored", img.Key1200)
		}
	}
	if _, ok := f.store.files[b.Key1200]; !ok {
		t.Error("the kept photo's files were deleted")
	}
}

func TestUpdateColoursNothingSaved(t *testing.T) {
	tests := []struct {
		name          string
		edit          func(bege, vinho domain.Colour) domain.ColoursEdit
		wantFields    []string
		wantStale     bool
		wantProcessed int
	}{
		{
			// Vinho exists, but this screen never showed it.
			name: "a colour added elsewhere",
			edit: func(bege, _ domain.Colour) domain.ColoursEdit {
				return domain.ColoursEdit{Colours: []domain.ColourEdit{keep(bege)}}
			},
			wantStale: true,
		},
		{
			name: "a colour deleted elsewhere",
			edit: func(bege, vinho domain.Colour) domain.ColoursEdit {
				gone := domain.ColourEdit{ID: uuid.New().String(), Name: "Azul"}
				return domain.ColoursEdit{Colours: []domain.ColourEdit{keep(bege), keep(vinho), gone}}
			},
			wantStale: true,
		},
		{
			name: "a photo added elsewhere to the second colour",
			edit: func(bege, vinho domain.Colour) domain.ColoursEdit {
				v := keep(vinho)
				v.Photos.Order = nil
				return domain.ColoursEdit{Colours: []domain.ColourEdit{keep(bege), v}}
			},
			wantStale: true,
		},
		{
			// Errors name the colour by its place on screen, so the form
			// can mark the right one.
			name: "a bad photo in the second colour and a repeated name",
			edit: func(bege, vinho domain.Colour) domain.ColoursEdit {
				v := keep(vinho)
				v.Name = "bege"
				v.Photos.New = []domain.PhotoSource{source("good"), source(notAPhoto)}
				v.Photos.Order = append(v.Photos.Order, domain.PhotoSlot{NewIndex: 0}, domain.PhotoSlot{NewIndex: 1})
				return domain.ColoursEdit{Colours: []domain.ColourEdit{keep(bege), v}}
			},
			wantFields: []string{
				domain.ColourField(1, "name"),
				domain.ColourField(1, domain.PhotoErrorField(1)),
			},
			wantProcessed: 1,
		},
		{
			name: "no colours left",
			edit: func(bege, vinho domain.Colour) domain.ColoursEdit {
				return domain.ColoursEdit{Removed: []string{bege.ID, vinho.ID}}
			},
			wantFields: []string{"colours"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newPhotoFixture(existing)
			bege, vinho := withTwoColours(f)
			before := f.repo.products[existing.ID]
			files := len(f.store.files)

			edit := tt.edit(bege, vinho)
			_, err := f.service.Update(context.Background(), before, &edit)
			if err == nil {
				t.Fatal("expected an error")
			}

			got := map[string]bool{}
			for _, field := range fieldsOf(err) {
				got[field] = true
			}
			for _, field := range tt.wantFields {
				if !got[field] {
					t.Errorf("no validation error on %s in %v", field, err)
				}
			}
			var stale *domain.StaleError
			if tt.wantStale != errors.As(err, &stale) {
				t.Errorf("err = %v, want StaleError: %v", err, tt.wantStale)
			}
			if f.processor.processed != tt.wantProcessed {
				t.Errorf("%d photos processed, want %d", f.processor.processed, tt.wantProcessed)
			}
			if f.repo.writes != 0 {
				t.Errorf("%d writes reached the repository", f.repo.writes)
			}
			if len(f.store.files) != files {
				t.Errorf("%d files stored, want the original %d", len(f.store.files), files)
			}
		})
	}
}

// fieldsOf lists the fields of the validation errors joined in err.
func fieldsOf(err error) []string {
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
		var v *domain.ValidationError
		if errors.As(err, &v) {
			fields = append(fields, v.Field)
		}
	}
	walk(err)
	return fields
}
