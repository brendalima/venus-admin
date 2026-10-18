package repository

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/brendalima/venus-admin/internal/core/domain"
)

func newImage() domain.Image {
	id := uuid.New().String()
	return domain.Image{ID: id, Key600: "produtos/" + id + "-600.jpg", Key1200: "produtos/" + id + "-1200.jpg"}
}

func imageIDs(images []domain.Image) []string {
	ids := make([]string, len(images))
	for i, img := range images {
		ids[i] = img.ID
	}
	return ids
}

func TestProductCreateWithImages(t *testing.T) {
	db := testDB(t)
	repo := MustNewProduct(db)
	ctx := context.Background()

	p := newTestProduct(t, db)
	a, b := newImage(), newImage()
	p.Images = []domain.Image{b, a}

	created, err := repo.Create(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if got := imageIDs(created.Images); !slices.Equal(got, []string{b.ID, a.ID}) {
		t.Errorf("created images %v, want [b a]", got)
	}
	stored, err := repo.GetByID(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := imageIDs(stored.Images); !slices.Equal(got, []string{b.ID, a.ID}) {
		t.Errorf("stored images %v, want [b a]", got)
	}
	if stored.Images[0].Key600 != b.Key600 || stored.Images[0].Position != 0 || stored.Images[1].Position != 1 {
		t.Errorf("stored cover %+v", stored.Images[0])
	}
}

// A product with three images, a, b and c in that order.
func productWithImages(t *testing.T, repo *Product, p domain.Product) (domain.Product, domain.Image, domain.Image, domain.Image) {
	t.Helper()
	a, b, c := newImage(), newImage(), newImage()
	p.Images = []domain.Image{a, b, c}
	created, err := repo.Create(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	return created, created.Images[0], created.Images[1], created.Images[2]
}

func TestProductUpdateWithImages(t *testing.T) {
	db := testDB(t)
	repo := MustNewProduct(db)
	ctx := context.Background()
	p, a, b, c := productWithImages(t, repo, newTestProduct(t, db))

	// c becomes the cover, b goes, d is new, and the name changes too.
	d := newImage()
	p.Name = "Novo nome"
	p.Images = []domain.Image{c, d, a}
	updated, gone, err := repo.UpdateWithImages(ctx, p, []string{d.ID}, []string{b.ID})
	if err != nil {
		t.Fatal(err)
	}
	if got := imageIDs(updated.Images); !slices.Equal(got, []string{c.ID, d.ID, a.ID}) {
		t.Errorf("returned images %v, want [c d a]", got)
	}
	if len(gone) != 1 || gone[0].ID != b.ID || gone[0].Key1200 != b.Key1200 {
		t.Errorf("removed %+v, want b with its keys", gone)
	}

	stored, err := repo.GetByID(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Name != "Novo nome" {
		t.Errorf("name = %q", stored.Name)
	}
	if got := imageIDs(stored.Images); !slices.Equal(got, []string{c.ID, d.ID, a.ID}) {
		t.Errorf("stored images %v, want [c d a]", got)
	}
	for i, img := range stored.Images {
		if int(img.Position) != i {
			t.Errorf("image %d stored at position %d", i, img.Position)
		}
	}
}

func TestProductUpdateWithImagesStale(t *testing.T) {
	db := testDB(t)
	repo := MustNewProduct(db)
	ctx := context.Background()

	tests := []struct {
		name string
		edit func(p *domain.Product, a, b, c domain.Image) (added, removed []string)
	}{
		{
			// b exists, but this screen never mentioned it: it was added
			// elsewhere after the page loaded.
			name: "unknown photo",
			edit: func(p *domain.Product, a, _, c domain.Image) ([]string, []string) {
				p.Images = []domain.Image{a, c}
				return nil, nil
			},
		},
		{
			// x was kept on screen, but no such photo exists any more.
			name: "kept photo deleted meanwhile",
			edit: func(p *domain.Product, a, b, c domain.Image) ([]string, []string) {
				p.Images = []domain.Image{a, b, c, newImage()}
				return nil, nil
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, a, b, c := productWithImages(t, repo, newTestProduct(t, db))
			p.Name = "Não deveria salvar"
			added, removed := tt.edit(&p, a, b, c)

			_, _, err := repo.UpdateWithImages(ctx, p, added, removed)
			var stale *domain.StaleError
			if !errors.As(err, &stale) {
				t.Fatalf("err = %v, want StaleError", err)
			}

			stored, err := repo.GetByID(ctx, p.ID)
			if err != nil {
				t.Fatal(err)
			}
			if stored.Name == "Não deveria salvar" {
				t.Error("the fields were saved anyway")
			}
			if got := imageIDs(stored.Images); !slices.Equal(got, []string{a.ID, b.ID, c.ID}) {
				t.Errorf("images %v, want them untouched", got)
			}
		})
	}
}

func TestProductUpdateKeepsImages(t *testing.T) {
	db := testDB(t)
	repo := MustNewProduct(db)
	ctx := context.Background()
	p, a, b, c := productWithImages(t, repo, newTestProduct(t, db))

	// The plain Update, as the sold-out toggle uses it, with a stale or
	// empty image list: it must not touch the images.
	p.Images = nil
	p.SoldOut = true
	updated, err := repo.Update(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if got := imageIDs(updated.Images); !slices.Equal(got, []string{a.ID, b.ID, c.ID}) {
		t.Errorf("images %v, want them untouched", got)
	}
}
