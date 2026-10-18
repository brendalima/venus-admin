package repository

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/brendalima/venus-admin/internal/core/domain"
)

func addTestImage(t *testing.T, repo *Image, productID string) domain.Image {
	t.Helper()
	id := uuid.New().String()
	img, err := repo.Add(context.Background(), domain.Image{
		ID: id, ProductID: productID, Key600: "produtos/" + id + "-600.jpg", Key1200: "produtos/" + id + "-1200.jpg",
		Position: 99, // ignored
	})
	if err != nil {
		t.Fatalf("adding image: %v", err)
	}
	return img
}

// imageOrder returns the product's image IDs as the product reads them.
func imageOrder(t *testing.T, products *Product, productID string) []string {
	t.Helper()
	p, err := products.GetByID(context.Background(), productID)
	if err != nil {
		t.Fatalf("getting product: %v", err)
	}
	ids := make([]string, len(p.Images))
	for i, img := range p.Images {
		ids[i] = img.ID
	}
	return ids
}

func TestImageAddAppends(t *testing.T) {
	db := testDB(t)
	products, images := MustNewProduct(db), MustNewImage(db)
	p, err := products.Create(context.Background(), newTestProduct(t, db))
	if err != nil {
		t.Fatal(err)
	}

	a, b := addTestImage(t, images, p.ID), addTestImage(t, images, p.ID)
	if a.Position != 0 || b.Position != 1 || a.CreatedAt.IsZero() {
		t.Errorf("positions %d and %d, want 0 and 1", a.Position, b.Position)
	}
	if got := imageOrder(t, products, p.ID); !slices.Equal(got, []string{a.ID, b.ID}) {
		t.Errorf("order %v, want [a b]", got)
	}

	var notFound *domain.NotFoundError
	for name, productID := range map[string]string{"unknown": uuid.New().String(), "malformed": "x"} {
		_, err := images.Add(context.Background(), domain.Image{ID: uuid.New().String(), ProductID: productID, Key600: "k", Key1200: "k"})
		if !errors.As(err, &notFound) {
			t.Errorf("%s product: err = %v, want NotFoundError", name, err)
		}
	}
}

func TestImageMove(t *testing.T) {
	db := testDB(t)
	products, images := MustNewProduct(db), MustNewImage(db)
	p, err := products.Create(context.Background(), newTestProduct(t, db))
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for range 4 {
		ids = append(ids, addTestImage(t, images, p.ID).ID)
	}
	a, b, c, d := ids[0], ids[1], ids[2], ids[3]

	steps := []struct {
		name  string
		id    string
		index int
		want  []string
	}{
		{name: "cover", id: c, index: 0, want: []string{c, a, b, d}},
		{name: "down one", id: c, index: 1, want: []string{a, c, b, d}},
		{name: "to the end", id: a, index: 3, want: []string{c, b, d, a}},
		{name: "past the end", id: c, index: 50, want: []string{b, d, a, c}},
		{name: "before the start", id: a, index: -3, want: []string{a, b, d, c}},
		{name: "where it is", id: b, index: 1, want: []string{a, b, d, c}},
	}
	for _, s := range steps {
		if err := images.Move(context.Background(), p.ID, s.id, s.index); err != nil {
			t.Fatalf("%s: %v", s.name, err)
		}
		if got := imageOrder(t, products, p.ID); !slices.Equal(got, s.want) {
			t.Fatalf("%s: order %v, want %v", s.name, got, s.want)
		}
	}

	var notFound *domain.NotFoundError
	other, err := products.Create(context.Background(), newTestProduct(t, db))
	if err != nil {
		t.Fatal(err)
	}
	if err := images.Move(context.Background(), other.ID, a, 0); !errors.As(err, &notFound) {
		t.Errorf("moving another product's image: err = %v, want NotFoundError", err)
	}
}

func TestImageDelete(t *testing.T) {
	db := testDB(t)
	products, images := MustNewProduct(db), MustNewImage(db)
	p, err := products.Create(context.Background(), newTestProduct(t, db))
	if err != nil {
		t.Fatal(err)
	}
	a, b := addTestImage(t, images, p.ID), addTestImage(t, images, p.ID)

	var notFound *domain.NotFoundError
	other, err := products.Create(context.Background(), newTestProduct(t, db))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := images.Delete(context.Background(), other.ID, a.ID); !errors.As(err, &notFound) {
		t.Errorf("deleting through another product: err = %v, want NotFoundError", err)
	}

	got, err := images.Delete(context.Background(), p.ID, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Key600 != a.Key600 || got.Key1200 != a.Key1200 {
		t.Errorf("deleted %+v, want the keys of %+v", got, a)
	}
	if order := imageOrder(t, products, p.ID); !slices.Equal(order, []string{b.ID}) {
		t.Errorf("order after delete %v, want [b]", order)
	}
	if _, err := images.Delete(context.Background(), p.ID, a.ID); !errors.As(err, &notFound) {
		t.Errorf("deleting twice: err = %v, want NotFoundError", err)
	}
}
